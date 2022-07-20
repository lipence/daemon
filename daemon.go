package daemon

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/lipence/graph"
	"github.com/tidwall/hashmap"
)

const (
	DefaultAppletClosureMaxRetry = 5
	DefaultAppletRestartInterval = time.Second
)

var (
	ErrDaemonIsInitialized      = fmt.Errorf("daemon is initialized")
	ErrDaemonNotInitialized     = fmt.Errorf("daemon not initialized")
	ErrDaemonRegisterIsFrozen   = fmt.Errorf("daemon register is frozen")
	ErrAppletIdentityConflict   = fmt.Errorf("applet identity conflict")
	ErrAppletQuitsWithoutReason = fmt.Errorf("applet quits without reason")
)

type daemonStatusCtx struct {
	didInit bool
	halting bool
	ctx     context.Context
	cancel  func()
}

func newDaemonStatusCtx() daemonStatusCtx {
	var cc = daemonStatusCtx{}
	cc.ctx, cc.cancel = context.WithCancel(context.Background())
	return cc
}

func (c *daemonStatusCtx) running() bool {
	return !c.halting && !(c.ctx != nil && errors.Is(context.Canceled, c.ctx.Err()))
}

func (c *daemonStatusCtx) setHalting() {
	c.halting = true
}

func (c *daemonStatusCtx) halt() {
	c.halting = true
	if c.cancel != nil {
		c.cancel()
	}
}

func (c *daemonStatusCtx) haltSig() <-chan struct{} {
	return c.ctx.Done()
}

func (c *daemonStatusCtx) setInitialized() bool {
	if c.didInit {
		return false
	}
	c.didInit = true
	return true
}

func (c *daemonStatusCtx) initialized() bool {
	return c.didInit
}

type daemonEscapingCtx struct {
	context.Context
	cancel context.CancelFunc
}

func newDaemonEscapingCtx() daemonEscapingCtx {
	var cc = daemonEscapingCtx{}
	cc.Context, cc.cancel = context.WithCancel(context.Background())
	return cc
}

func (c daemonEscapingCtx) escape() {
	c.cancel()
}

type Daemon[id ID] struct {
	serveMaxRetry   int
	closureMaxRetry int
	restartInterval time.Duration
	logger          logger
	applets         hashmap.Map[id, Applet[id]]
	appGraph        graph.Graph[id]
	status          daemonStatusCtx
}

func NewDaemon[id ID](opts ...OptionPayload[id]) *Daemon[id] {
	var d = &Daemon[id]{
		serveMaxRetry:   DefaultAppletClosureMaxRetry,
		closureMaxRetry: DefaultAppletClosureMaxRetry,
		restartInterval: DefaultAppletRestartInterval,
		logger:          defaultLogger,
		status:          newDaemonStatusCtx(),
	}
	for _, opt := range opts {
		opt.apply(d)
	}
	return d
}

func (d *Daemon[id]) Register(applets ...Applet[id]) error {
	if d.status.initialized() {
		return ErrDaemonRegisterIsFrozen
	}
	for _, applet := range applets {
		var identity = applet.Identity()
		if _, exist := d.applets.Get(identity); exist {
			return fmt.Errorf("%w, applet = %v", ErrAppletIdentityConflict, identity)
		}
		d.applets.Set(identity, applet)
	}
	return nil
}

func (d *Daemon[id]) Init() error {
	if !d.status.setInitialized() {
		return ErrDaemonIsInitialized
	}
	var applets = d.applets.Values()
	// build dep graph
	d.appGraph = graph.NewDirected[id]()
	for _, applet := range applets {
		d.appGraph.AddNode(applet.Identity())
	}
	var errs errorCollection
	for _, applet := range applets {
		for _, dep := range applet.Depends() {
			if err := d.appGraph.AddEdge(applet.Identity(), dep, 0); err != nil {
				errs.append(fmt.Errorf("%w: app: %s, dep: %s", err, applet.Identity(), dep))
			}
		}
	}
	if err := errs.batch(); err != nil {
		return err
	}
	// init applets
	for _, applet := range applets {
		if err := applet.Initialize(); err != nil {
			errs.append(fmt.Errorf("%w: applet = %s", err, applet.Identity()))
		}
	}
	return errs.batch()
}

func (d *Daemon[id]) sortDependency(target id) (appletsList []Applet[id], err error) {
	var idList []id
	if target == empty[id]() {
		idList, err = graph.NewRuntime[id](d.appGraph).DAGSortAll()
	} else {
		idList, err = graph.NewRuntime[id](d.appGraph).DAGSort(target)
	}
	if err != nil {
		return nil, err
	}
	appletsList = make([]Applet[id], len(idList))
	for i, appId := range idList {
		appletsList[i], _ = d.applets.Get(appId)
	}
	return appletsList, err
}

func (d *Daemon[id]) Serve() (err error) {
	if !d.status.initialized() {
		return ErrDaemonNotInitialized
	}
	defer d.status.halt()

	var applets []Applet[id]
	if applets, err = d.sortDependency(empty[id]()); err != nil {
		return err
	} else {
		reverse(applets)
	}
	var escapeCtx = newDaemonEscapingCtx()
	defer escapeCtx.escape()

	var errs errorCollection
	var wg sync.WaitGroup
startupLoop:
	for i := len(applets) - 1; i >= 0; i-- {
		wg.Add(1)
		var _applet = applets[i]
		var _appletStartCtx, _appletStartOk = context.WithCancel(context.Background())
		defer _appletStartOk()
		go func() {
			if err = d.serveApplet(_applet, escapeCtx, _appletStartOk); err != nil {
				errs.append(err)
			}
			wg.Done()
		}()
		select {
		case <-escapeCtx.Done():
			break startupLoop
		case <-_appletStartCtx.Done():
			continue
		case <-time.After(time.Minute):
			errs.append(fmt.Errorf("applet startup timeout: id = %s", _applet.Identity()))
			escapeCtx.escape()
		}
	}
	wg.Wait()
	return errs.batch()
}

func (d *Daemon[id]) Shutdown(ctx context.Context) (err error) {
	if !d.status.running() {
		return nil
	}
	d.status.setHalting()

	var applets []Applet[id]
	if applets, err = d.sortDependency(empty[id]()); err != nil {
		return err
	}

	var errs errorCollection
	for i := len(applets) - 1; i >= 0; i-- {
		if err = d.shutdownApplet(applets[i], ctx); err != nil {
			errs.append(fmt.Errorf("%w: applet = %s", err, applets[i].Identity()))
		}
	}
	return errs.batch()
}

func (d *Daemon[id]) serveApplet(applet Applet[id], escCtx daemonEscapingCtx, startOk func()) (err error) {
	defer func() {
		if err != nil {
			escCtx.escape()
			err = fmt.Errorf("[applet::%s] %w", applet.Identity(), err)
		}
	}()
	var lifeCount int
	var appStarted = &atomicBool{}
	var appIdentity = applet.Identity()
	var appWrapper = &appletServeWrapper[id]{applet: applet, startOk: func() {
		if appStarted.CAS(true) {
			startOk()
			d.logger.Infof("applet %q started", applet.Identity())
		}
	}}
	var appRestartInterval = ternary(d.restartInterval != 0, d.restartInterval, DefaultAppletRestartInterval)
	var appRestartMaxRetry = ternary(d.serveMaxRetry > 0, d.serveMaxRetry, DefaultAppletClosureMaxRetry)
retryLoop:
	for lifeCount = 0; d.status.running(); lifeCount++ {
		switch {
		case lifeCount >= appRestartMaxRetry:
			// exceed max fail times
			d.logger.Errorf("applet %q bootstrap failed after maximum(%d) retries.", appIdentity, appRestartMaxRetry)
			err = ternary(appWrapper.lastErr != nil, appWrapper.lastErr, ErrAppletQuitsWithoutReason)
			return err
		case lifeCount > 0:
			if appWrapper.fail {
				err = ternary(appWrapper.lastErr != nil, appWrapper.lastErr, ErrAppletQuitsWithoutReason)
				return err
			}
			if !appWrapper.reStart {
				return nil
			}
		}
		time.Sleep(appRestartInterval)
		if !d.status.running() {
			return nil
		}
		select {
		case <-escCtx.Done():
			return nil
		case <-d.status.haltSig():
			d.logger.Warnf("applet %q halt without closure", appIdentity)
			break retryLoop
		case err = <-async(appWrapper.serve):
			applet.OnQuit(appWrapper)
			var actDesc string
			if d.status.running() {
				actDesc = appWrapper.Description(appRestartInterval)
			} else {
				actDesc = "daemon shutting down"
			}
			if err != nil {
				d.logger.Warnf("applet %q quit with error (%s): %v", appIdentity, actDesc, err)
			} else {
				d.logger.Warnf("applet %q quit without error (%s)", appIdentity, actDesc)
			}
		}
	}
	return nil
}

func (d *Daemon[id]) shutdownApplet(applet Applet[id], ctx context.Context) (err error) {
	for i := 0; true; i++ {
		switch {
		case i >= ternary(d.closureMaxRetry > 0, d.closureMaxRetry, DefaultAppletClosureMaxRetry):
			d.logger.Errorf("applet %s shutdown failed with repeatedly retries.")
			return nil
		case !applet.Serving():
			return nil
		}
		d.logger.Warnf("applet %q is going to shutdown", applet.Identity())
		if err = applet.Shutdown(ctx); err == nil {
			return nil
		} else {
			d.logger.Warnf("applet %q shutdown fail: %v", applet.Identity(), err)
		}
		time.Sleep(ternary(d.restartInterval != 0, d.restartInterval, DefaultAppletRestartInterval))
	}
	return err
}

func (d *Daemon[id]) Halt() { d.status.halt() }
