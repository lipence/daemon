package daemon

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/samber/lo"
	"github.com/samber/lo/parallel"
	"go.uber.org/atomic"
)

const (
	DefaultAppletClosureMaxRetry = 5
	DefaultAppletRestartInterval = time.Second
)

var (
	ErrDaemonIsInitialized      = fmt.Errorf("daemon is initialized")
	ErrDaemonNotInitialized     = fmt.Errorf("daemon not initialized")
	ErrDaemonRegisterIsFrozen   = fmt.Errorf("daemon register is frozen")
	ErrAppletQuitsWithoutReason = fmt.Errorf("applet quits without reason")
)

type daemonStatusCtx struct {
	halting bool
	ctx     context.Context
	cFunc   func()
}

func (c *daemonStatusCtx) running() bool {
	return !c.halting && !(c.ctx != nil && errors.Is(context.Canceled, c.ctx.Err()))
}

func (c *daemonStatusCtx) setHalting() {
	c.halting = true
}

func (c *daemonStatusCtx) halt() {
	c.halting = true
	if c.cFunc != nil {
		c.cFunc()
	}
}

func (c *daemonStatusCtx) haltSig() <-chan struct{} {
	return c.ctx.Done()
}

func NewDaemon(opts ...OptionPayload) *Daemon {
	var d = &Daemon{
		serveMaxRetry:   DefaultAppletClosureMaxRetry,
		closureMaxRetry: DefaultAppletClosureMaxRetry,
		restartInterval: DefaultAppletRestartInterval,
		serveCtx:        &daemonStatusCtx{ctx: context.Background()},
		initCalled:      atomic.NewBool(false),
		errorBatch:      defaultErrorBatch,
		logger:          defaultLogger,
	}
	for _, opt := range opts {
		opt.apply(d)
	}
	d.serveCtx.ctx, d.serveCtx.cFunc = context.WithCancel(d.serveCtx.ctx)
	return d
}

type Daemon struct {
	serveMaxRetry   int
	closureMaxRetry int
	restartInterval time.Duration
	applets         []Applet
	initCalled      *atomic.Bool
	serveCtx        *daemonStatusCtx
	errorBatch      errorBatchFunc
	logger          logger
}

func (d *Daemon) Register(applets ...Applet) error {
	if d.initCalled.Load() {
		return ErrDaemonRegisterIsFrozen
	}
	d.applets = append(d.applets, applets...)
	return nil
}

func (d *Daemon) Init() error {
	if !d.initCalled.CAS(false, true) {
		return ErrDaemonIsInitialized
	}
	return d.errorBatch(parallel.Map(d.applets, func(applet Applet, _ int) error {
		if err := applet.Initialize(); err != nil {
			return fmt.Errorf("[applet::%s] %w", applet.Identity(), err)
		}
		return nil
	}))
}

type appletWithCancelContext = lo.Tuple3[Applet, context.Context, context.CancelFunc]
type appletWithContext = lo.Tuple2[Applet, context.Context]

func (d *Daemon) Serve() error {
	if !d.initCalled.Load() {
		return ErrDaemonNotInitialized
	}
	defer d.serveCtx.halt()
	var failFastCtx, failFastFunc = context.WithCancel(context.Background())
	defer failFastFunc()
	var appletWithFastFailSigArr = make([]appletWithCancelContext, len(d.applets))
	for i := 0; i < len(d.applets); i++ {
		appletWithFastFailSigArr[i] = appletWithCancelContext{d.applets[i], failFastCtx, failFastFunc}
	}
	return d.errorBatch(parallel.Map(appletWithFastFailSigArr, d.serveApplet))
}

func (d *Daemon) Shutdown(ctx context.Context) error {
	if !d.serveCtx.running() {
		return nil
	}
	d.serveCtx.setHalting()
	var appletWithContextArr = make([]appletWithContext, len(d.applets))
	for i := 0; i < len(d.applets); i++ {
		appletWithContextArr[i] = appletWithContext{d.applets[i], ctx}
	}
	return d.errorBatch(parallel.Map(appletWithContextArr, d.shutdownApplet))
}

func (d *Daemon) serveApplet(t appletWithCancelContext, _ int) (err error) {
	var applet, failFastCtx, failFastFunc = lo.Unpack3(t)
	defer func() {
		if err != nil {
			failFastFunc()
			err = fmt.Errorf("[applet::%s] %w", applet.Identity(), err)
		}
	}()
	var appIdentity = applet.Identity()
	var appWrapper = &appletServeWrapper{applet: applet}
	var appRestartInterval = lo.Ternary(d.restartInterval != 0, d.restartInterval, DefaultAppletRestartInterval)
retryLoop:
	for i := 0; d.serveCtx.running(); i++ {
		switch {
		case i >= lo.Ternary(d.serveMaxRetry > 0, d.serveMaxRetry, DefaultAppletClosureMaxRetry):
			// exceed max fail times
			d.logger.Errorf("applet `%s` bootstrap failed with repeatedly retries.", appIdentity)
			err = lo.Ternary(appWrapper.lastErr != nil, appWrapper.lastErr, ErrAppletQuitsWithoutReason)
			return err
		case i == 0:
			// first startup
			d.logger.Infof("starting `%s` ( %s )", appIdentity, appIdentity.UUID())
		case appWrapper.fail:
			// fail with err
			err = lo.Ternary(appWrapper.lastErr != nil, appWrapper.lastErr, ErrAppletQuitsWithoutReason)
			return err
		case appWrapper.reStart:
			// restart
			time.Sleep(appRestartInterval)
			if !d.serveCtx.running() {
				return nil
			}
			d.logger.Warnf("restarting `%s`", appIdentity)
		default:
			// normally quits
			return nil
		}
		select {
		case <-failFastCtx.Done():
			return nil
		case <-d.serveCtx.haltSig():
			d.logger.Warnf("applet `%s` halt without closure", appIdentity)
			break retryLoop
		case err = <-lo.Async(appWrapper.serve):
			applet.OnQuit(appWrapper)
			var actDesc string
			if d.serveCtx.running() {
				actDesc = appWrapper.Description(appRestartInterval)
			} else {
				actDesc = "daemon shutting down"
			}
			if err != nil {
				d.logger.Warnf("applet `%s` quit with error (%s): %v", appIdentity, actDesc, err)
			} else {
				d.logger.Warnf("applet `%s` quit no error (%s)", appIdentity, actDesc)
			}
		}
	}
	return nil
}

func (d *Daemon) shutdownApplet(t appletWithContext, _ int) (err error) {
	var applet, ctx = lo.Unpack2(t)
	for i := 0; true; i++ {
		switch {
		case i >= lo.Ternary(d.closureMaxRetry > 0, d.closureMaxRetry, DefaultAppletClosureMaxRetry):
			d.logger.Errorf("applet %s shutdown failed with repeatedly retries.")
			return nil
		case !applet.Serving():
			return nil
		}
		d.logger.Warnf("shutting down %s", applet.Identity())
		if err = applet.Shutdown(ctx); err == nil {
			d.logger.Warnf("applet `%s` quits with no error", applet.Identity())
			return nil
		} else {
			d.logger.Warnf("applet `%s` shutdown fail: %v", applet.Identity(), err)
		}
		time.Sleep(lo.Ternary(d.restartInterval != 0, d.restartInterval, DefaultAppletRestartInterval))
	}
	return err
}

func (d *Daemon) Halt() {
	d.serveCtx.halt()
}
