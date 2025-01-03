package api

import (
	"encoding/json"
	"net/http"

	"github.com/aagat/attic/backend/config"
	"github.com/aagat/attic/backend/extractor"
	"github.com/aagat/attic/backend/formatter"
	"github.com/aagat/attic/backend/integrations"
	"github.com/aagat/attic/backend/integrations/llm"
	"github.com/aagat/attic/backend/mailer"
	"github.com/aagat/attic/backend/models"
)

type handler struct {
	extractor *extractor.Extractor
	formatter *formatter.Formatter
	mailer    *mailer.Mailer
}

func NewHandler(cfg *config.Config) (*handler, error) {
	// Initialize LLM client
	llmClient, err := llm.NewGeminiClient(llm.Config{
		APIKey: cfg.LLMAPIKey,
		Model:  "gemini-pro-vision",
	})
	if err != nil {
		return nil, err
	}

	// Initialize Puppeteer
	puppeteer := integrations.NewPuppeteer("scripts/puppeteer.js")

	// Initialize components
	ext := extractor.NewExtractor(puppeteer, llmClient)
	fmt, err := formatter.NewFormatter()
	if err != nil {
		return nil, err
	}
	mlr := mailer.NewMailer(cfg.KindleEmail, cfg.SenderEmail, cfg.SenderPassword)

	return &handler{
		extractor: ext,
		formatter: fmt,
		mailer:    mlr,
	}, nil
}

// AddToKindleHandler processes requests to convert and send content to Kindle
func (h *handler) AddToKindleHandler(w http.ResponseWriter, r *http.Request) {
	// Parse multipart form
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid request format")
		return
	}

	// Get request type
	requestType := r.FormValue("type")
	if requestType != "url" && requestType != "file" {
		respondWithError(w, http.StatusBadRequest, "Invalid request type")
		return
	}

	var content []byte
	var err error

	// Extract content based on type
	if requestType == "url" {
		url := r.FormValue("url")
		content, err = h.extractor.ExtractFromURL(url)
	} else {
		file, _, err := r.FormFile("file")
		if err != nil {
			respondWithError(w, http.StatusBadRequest, "Failed to read file")
			return
		}
		defer file.Close()
		content, err = h.extractor.ExtractFromFile(file)
	}

	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to extract content")
		return
	}

	// Format content to PDF
	pdf, err := h.formatter.FormatToPDF(content)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to format content")
		return
	}

	// Send to Kindle
	if err := h.mailer.SendToKindle(pdf); err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to send to Kindle")
		return
	}

	// Respond with success
	response := models.AddToKindleResponse{
		Status:  "success",
		Message: "Content successfully sent to Kindle",
	}
	respondWithJSON(w, http.StatusOK, response)
}

func respondWithError(w http.ResponseWriter, code int, message string) {
	respondWithJSON(w, code, map[string]string{"error": message})
}

func respondWithJSON(w http.ResponseWriter, code int, payload interface{}) {
	response, _ := json.Marshal(payload)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	w.Write(response)
}
