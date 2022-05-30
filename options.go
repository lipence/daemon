package daemon

import (
	"context"
)

type OptionPayload interface {
	apply(*Daemon)
}

func OptLogger(logger logger) OptionPayload {
	return &optLogger{logger: logger}
}

type optLogger struct {
	logger logger
}

func (o *optLogger) apply(d *Daemon) {
	d.logger = o.logger
}

func OptContext(ctx context.Context) OptionPayload {
	return &optContext{ctx: ctx}
}

type optContext struct {
	ctx context.Context
}

func (o *optContext) apply(d *Daemon) {
	d.serveCtx.ctx = o.ctx
}

func OptMaxRetry(serve, closure int) OptionPayload {
	return &optMaxRetry{serve: serve, closure: closure}
}

type optMaxRetry struct {
	serve, closure int
}

func (o *optMaxRetry) apply(d *Daemon) {
	d.serveMaxRetry, d.closureMaxRetry = o.serve, o.closure
}

func OptErrorBatchFunc(batchFunc errorBatchFunc) OptionPayload {
	return &optErrorBatchFunc{batchFunc: batchFunc}
}

type optErrorBatchFunc struct {
	batchFunc errorBatchFunc
}

func (o *optErrorBatchFunc) apply(d *Daemon) {
	d.errorBatch = o.batchFunc
}
