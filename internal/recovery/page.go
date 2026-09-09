package recovery

import (
	"attic/internal/acquisition"
	"context"
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
	saved, err := p.Browser.Recover(ctx, raw)
	if err != nil {
		return acquisition.RenderedPage{}, err
	}
	return p.Renderer.RenderSaved(ctx, saved.FinalURL, saved.HTML)
}
