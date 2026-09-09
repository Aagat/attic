package capture

import "attic/internal/acquisition"

// FromPage sanitizes a server recovery snapshot with the same rules as captures.
func FromPage(page acquisition.RenderedPage) (Result, error) { return convert(page) }
