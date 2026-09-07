package ai

import (
	"crypto/sha256"
	"fmt"
	"regexp"
	"strings"
)

var embeddedImage = regexp.MustCompile(`data:image/(?:png|jpeg|webp);base64,[A-Za-z0-9+/=]+`)

// The model sees stable image references, not hundreds of thousands of base64
// tokens. Only image bytes already in the candidate can be restored afterward;
// replacement markup still passes through the security sanitizer.
func imageReferences(markup string) (string, map[string]string) {
	images := make(map[string]string)
	clean := embeddedImage.ReplaceAllStringFunc(markup, func(data string) string {
		ref := fmt.Sprintf("attic-image:%x", sha256.Sum256([]byte(data)))
		images[ref] = data
		return ref
	})
	return clean, images
}

func restoreImages(markup, candidate string) string {
	_, images := imageReferences(candidate)
	for ref, data := range images {
		markup = strings.ReplaceAll(markup, ref, data)
	}
	return markup
}
