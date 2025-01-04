package api

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/aagat/attic/backend/config"
	"github.com/aagat/attic/backend/extractor"
	"github.com/aagat/attic/backend/formatter"
	"github.com/aagat/attic/backend/integrations"
	"github.com/aagat/attic/backend/integrations/llm"
	"github.com/aagat/attic/backend/mailer"
)

// AddToKindleHandler processes requests to convert and send content to Kindle
func AddToKindleHandler(w http.ResponseWriter, r *http.Request) {
	// Get configuration
	cfg, err := config.Load()
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to load configuration")
		return
	}

	// Initialize LLM client
	llmClient, err := llm.NewGeminiClient(llm.Config{
		APIKey: cfg.LLMAPIKey,
		Model:  cfg.Model,
	})
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to initialize LLM client")
		return
	}

	// Initialize Puppeteer
	puppeteer, err := integrations.NewPuppeteer()
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to initialize Puppeteer")
		return
	}

	// Initialize extractor
	extractor := extractor.NewExtractor(puppeteer, llmClient)

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

	// Extract content based on type
	if requestType == "url" {
		url := r.FormValue("url")
		content, err = extractor.ExtractFromURL(url)
	} else {
		file, _, err := r.FormFile("file")
		if err != nil {
			respondWithError(w, http.StatusBadRequest, "Failed to read file")
			return
		}
		defer file.Close()
		content, err = extractor.ExtractFromFile(file, r.FormValue("filename"))
	}

	if err != nil {
		log.Printf("Content extraction failed: %v", err)
		respondWithError(w, http.StatusInternalServerError, "Failed to extract content")
		return
	}

	// Format content to PDF
	pdfFormatter, err := formatter.NewFormatter()
	if err != nil {
		log.Printf("Failed to initialize formatter: %v", err)
		respondWithError(w, http.StatusInternalServerError, "Failed to initialize formatter")
		return
	}

	pdf, err := pdfFormatter.FormatToPDF(content)
	if err != nil {
		log.Printf("PDF formatting failed: %v", err)
		respondWithError(w, http.StatusInternalServerError, "Failed to format content")
		return
	}

	// Send to Kindle
	mailer := mailer.NewMailer(cfg.KindleEmail, cfg.SenderEmail, cfg.SenderPassword)
	if err := mailer.SendToKindle(pdf); err != nil {
		log.Printf("Failed to send to Kindle: %v", err)
		respondWithError(w, http.StatusInternalServerError, "Failed to send to Kindle")
		return
	}

	// Respond with success
	response := map[string]string{
		"status":  "success",
		"message": "Content successfully sent to Kindle",
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
