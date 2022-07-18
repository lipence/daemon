package daemon

import (
	"context"
)

type OptionPayload[id ID] interface {
	apply(*Daemon[id])
}

func OptLogger[id ID](logger logger) OptionPayload[id] {
	return &optLogger[id]{logger: logger}
}

type optLogger[id ID] struct {
	logger logger
}

func (o *optLogger[id]) apply(d *Daemon[id]) {
	d.logger = o.logger
}

func OptContext[id ID](ctx context.Context) OptionPayload[id] {
	return &optContext[id]{ctx: ctx}
}

type optContext[id ID] struct {
	ctx context.Context
}

func (o *optContext[id]) apply(d *Daemon[id]) {
	d.status.ctx = o.ctx
}

func OptMaxRetry[id ID](serve, closure int) OptionPayload[id] {
	return &optMaxRetry[id]{serve: serve, closure: closure}
}

type optMaxRetry[id ID] struct {
	serve, closure int
}

func (o *optMaxRetry[id]) apply(d *Daemon[id]) {
	d.serveMaxRetry, d.closureMaxRetry = o.serve, o.closure
}
