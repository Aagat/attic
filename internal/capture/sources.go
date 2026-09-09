package capture

import (
	"context"
	"iter"

	"attic/internal/acquisition"
)

// Source is one retrieval attempt. Static content has already been sanitized;
// a rendered Page still needs the consumer's acceptance check. Fallback content
// must not stop attempts to retrieve a more complete copy.
type Source struct {
	URL      string
	Page     acquisition.RenderedPage
	Static   *Result
	Err      error
	Fallback bool
	original string
}

// Result applies preservation checks, independently of PDF article approval.
func (s Source) Result() (Result, error) {
	if s.Err != nil {
		return Result{}, s.Err
	}
	if s.Static != nil {
		return *s.Static, nil
	}
	result, err := convert(s.Page)
	result.OriginalURL = s.original
	return result, err
}

// Sources owns retrieval order and limits for preservation and PDF preparation.
// Discovery is lazy: accepting a source stops all further network requests.
// Consumers own their total deadline and their distinct acceptance requirements.
func (c *Capturer) Sources(ctx context.Context, original string) iter.Seq[Source] {
	return func(yield func(Source) bool) {
		if ctx.Err() != nil {
			return
		}
		emit := func(source Source) bool { source.original = original; return ctx.Err() == nil && yield(source) }
		mirror := xMirrorURL(original)
		if mirror != "" {
			result, err := c.xMirror(ctx, original, mirror)
			if !emit(Source{URL: mirror, Static: &result, Err: err}) {
				return
			}
		}
		if endpoint := xEmbedURL(original); endpoint != "" && ctx.Err() == nil {
			result, err := c.xEmbed(ctx, original, endpoint)
			if !emit(Source{URL: endpoint, Static: &result, Err: err, Fallback: true}) {
				return
			}
			if err == nil {
				canonical := xMirrorURL(result.FinalURL)
				if canonical != "" && canonical != mirror {
					recovered, err := c.xMirror(ctx, original, canonical)
					if !emit(Source{URL: canonical, Static: &recovered, Err: err}) {
						return
					}
				}
			}
		}
		if ctx.Err() != nil {
			return
		}
		page, err := c.renderer.Snapshot(ctx, original)
		if !emit(Source{URL: original, Page: page, Err: err}) {
			return
		}
		if c.archives == nil || ctx.Err() != nil {
			return
		}
		seen := map[string]bool{original: true}
		attempts := 0
		for _, source := range c.archives.Candidates(ctx, original) {
			if ctx.Err() != nil || attempts == 3 {
				return
			}
			if source == "" || seen[source] {
				continue
			}
			seen[source] = true
			attempts++
			page, err := c.renderer.Snapshot(ctx, source)
			if !emit(Source{URL: source, Page: page, Err: err}) {
				return
			}
		}
	}
}
