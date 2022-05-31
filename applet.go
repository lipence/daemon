package daemon

import (
	"context"
	"fmt"
	"runtime/debug"
	"time"
)

type Applet interface {
	Identity() *Identity
	Initialize() (err error)
	OnQuit(ctx CtxAppletOnQuit)
	Serve() (err error)
	Serving() bool
	Shutdown(ctx context.Context) (err error)
}

type CtxAppletOnQuit interface {
	ReInitialize(do bool)
	ReStart(do bool)
	Fail(do bool, filter func(err error) error)
}

type appletServeWrapper struct {
	applet  Applet
	lastErr error
	reInit  bool
	reStart bool
	fail    bool
}

func (w *appletServeWrapper) serve() (err error) {
	defer func() {
		if _err := recover(); _err != nil {
			err = fmt.Errorf("panic occurred: %v, trace:\n%s", anyAsErr(_err), string(debug.Stack()))
		}
		if err != nil {
			w.lastErr = err
		}
	}()
	if w.reInit {
		if err = w.applet.Initialize(); err != nil {
			return err
		}
	}
	if err = w.applet.Serve(); err != nil {
		return err
	}
	return nil
}

func (w *appletServeWrapper) LastError() error {
	return w.lastErr
}

func (w *appletServeWrapper) ReInitialize(do bool) {
	w.reInit = do
}

func (w *appletServeWrapper) ReStart(do bool) {
	w.reStart = do
}

func (w *appletServeWrapper) Fail(do bool, filter func(err error) error) {
	if w.fail = do; w.fail && filter != nil {
		w.lastErr = filter(w.lastErr)
	}
}

func (w *appletServeWrapper) Description(after time.Duration) string {
	switch {
	case w.fail:
		return "fail and exit"
	case w.reStart:
		if w.reInit {
			return fmt.Sprintf("going to restart and re-init after %s", after)
		}
		return fmt.Sprintf("going to restart without re-init after %s", after)
	default:
		return "normally stop"
	}
}
