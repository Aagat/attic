package models

// AddToKindleRequest represents the request to add content to Kindle
type AddToKindleRequest struct {
	Type      string `json:"type"`       // "url" or "file"
	URL       string `json:"url"`        // Required if type is "url"
	SourceURL string `json:"source_url"` // Optional, original URL if redirected
	Filename  string `json:"filename"`   // Optional, original filename for files
}

// AddToKindleResponse represents the response from the add-to-kindle endpoint
type AddToKindleResponse struct {
	Status     string `json:"status"`      // "success", "pending", or "error"
	Message    string `json:"message"`     // A descriptive message
	TrackingID string `json:"tracking_id"` // Optional tracking ID for async processing
}
