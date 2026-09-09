package acquisition

import (
	_ "embed"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

// The script only expands the requested X post, with bounded attempts. Any
// remaining expansion control marks the snapshot as truncated.
func expandPost(truncated *bool) chromedp.Action {
	return chromedp.Evaluate(postExpandScript, truncated, func(p *runtime.EvaluateParams) *runtime.EvaluateParams { return p.WithAwaitPromise(true) })
}

//go:embed post_expand.js
var postExpandScript string
