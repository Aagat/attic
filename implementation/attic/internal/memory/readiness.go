package memory

import (
	"context"
	"errors"
	"sync/atomic"
)

type Readiness struct {
	ready atomic.Bool
}

func NewReadiness(ready bool) *Readiness {
	r := &Readiness{}
	r.ready.Store(ready)
	return r
}

func (r *Readiness) SetReady(ready bool) {
	r.ready.Store(ready)
}

func (r *Readiness) Live(context.Context) error {
	return nil
}

func (r *Readiness) Ready(ctx context.Context) error {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	if !r.ready.Load() {
		return errors.New("not ready")
	}
	return nil
}
