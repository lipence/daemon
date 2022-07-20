package daemon

import (
	"context"
	"errors"
)

var ErrAlreadyServing = errors.New("already serving")

func NewCancelableApplet[id ID](core cancelableCore, appId id, deps ...id) Applet[id] {
	return &cancelableWrapper[id]{
		id:   appId,
		deps: deps,
		core: core,
	}
}

type cancelableCore interface {
	Init() error
	Serve(serveCtx context.Context, onStartup func()) error
	Stop(stopCtx context.Context) error
}

type cancelableWrapper[id ID] struct {
	id      id
	deps    []id
	status  atomicBool
	statCtx context.Context
	statCan context.CancelFunc
	core    cancelableCore
}

func (w *cancelableWrapper[id]) Identity() id { return w.id }

func (w *cancelableWrapper[id]) Depends() []id { return w.deps }

func (w *cancelableWrapper[id]) OnQuit(ctx CtxAppletOnQuit) {
	ctx.ReStart(true)
	ctx.ReInitialize(true)
}

func (w *cancelableWrapper[id]) Initialize() (err error) {
	if w.status.Load() {
		return ErrAlreadyServing
	}
	w.statCtx, w.statCan = context.WithCancel(context.Background())
	return w.core.Init()
}

func (w *cancelableWrapper[id]) Shutdown(ctx context.Context) (err error) {
	w.statCan()
	return w.core.Stop(ctx)
}

func (w *cancelableWrapper[id]) Serving() bool { return w.status.Load() }

func (w *cancelableWrapper[id]) Serve(serveOK func()) (err error) {
	if !w.status.CAS(true) {
		return ErrAlreadyServing
	}
	defer func() {
		w.status.Store(false)
	}()
	return w.core.Serve(w.statCtx, serveOK)
}
