package recovery

import (
	"attic/internal/acquisition"
	"context"
	"errors"
	"time"
)

// PageRecovery gives PDF jobs the same recovery session as bookmark capture.
// Recovered content is always rendered offline before mandatory article approval.
type PageRecovery struct {
	Browser  *Manager
	Renderer interface {
		RenderSaved(context.Context, string, []byte) (acquisition.RenderedPage, error)
	}
}

func (p PageRecovery) Recover(ctx context.Context, raw string) (acquisition.RenderedPage, error) {
	a, err := p.Browser.begin(raw)
	if err != nil {
		return acquisition.RenderedPage{}, err
	}
	deadline := time.NewTimer(100 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return acquisition.RenderedPage{}, ctx.Err()
		case <-deadline.C:
			return acquisition.RenderedPage{}, errors.New("browser recovery needs more time")
		case <-a.done:
			p.Browser.mu.Lock()
			saved := a.result
			p.Browser.mu.Unlock()
			if len(saved.HTML) == 0 {
				return acquisition.RenderedPage{}, errors.New("browser recovery needs input")
			}
			return p.Renderer.RenderSaved(ctx, saved.FinalURL, saved.HTML)
		case <-tick.C:
			if p.Browser.State(raw).Status == "needs_input" {
				return acquisition.RenderedPage{}, errors.New("browser recovery needs input")
			}
		}
	}
}
