package application

import (
	"attic/internal/domain"
	"attic/internal/sanitize"
	"golang.org/x/net/html"
	"strings"
)

// ApproveReadingEdit validates an explicit owner edit without asking AI to
// rewrite it. Automatic extraction continues through the AI approval path.
func ApproveReadingEdit(draft ArticleDraft) (ApprovedArticle, error) {
	body, err := sanitize.SanitizeHTML(draft.SemanticHTML)
	if err != nil || strings.TrimSpace(draft.Title) == "" {
		return ApprovedArticle{}, NewProcessingError(string(domain.FailureInsufficientContent), "Keep a title and readable content in the reading version", false)
	}
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		return ApprovedArticle{}, err
	}
	var text strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			text.WriteString(n.Data)
			text.WriteByte(' ')
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)
	draft.SemanticHTML = body
	draft.PlainText = strings.TrimSpace(text.String())
	draft.ExtractionMethod = "owner_edit"
	draft.AIAttemptID = ""
	draft.AIConfidence = 0
	draft.AICompleteness = 0
	return ApprovedArticle{draft: draft, valid: true}, nil
}
