package api

import (
	"encoding/json"
	"log"
	"mime/multipart"
	"net/http"
	"os"

	"github.com/aagat/attic/backend/config"
	"github.com/aagat/attic/backend/extractor"
	"github.com/aagat/attic/backend/formatter"
	"github.com/aagat/attic/backend/integrations"
	"github.com/aagat/attic/backend/integrations/llm"
	"github.com/aagat/attic/backend/mailer"
)

// AddToKindleHandler processes requests to convert and send content to Kindle
func AddToKindleHandler(w http.ResponseWriter, r *http.Request) {
	var err error

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
	e := extractor.NewExtractor(puppeteer, llmClient)

	// Parse multipart form
	if err = r.ParseMultipartForm(32 << 20); err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid request format")
		return
	}

	// Get request type
	requestType := r.FormValue("type")
	if requestType != "url" && requestType != "file" {
		respondWithError(w, http.StatusBadRequest, "Invalid request type")
		return
	}

	var extracted *extractor.ExtractedContent

	// Extract content based on type
	if requestType == "url" {
		url := r.FormValue("url")
		extracted, err = e.ExtractFromURL(url)
	} else {
		var file multipart.File
		var header *multipart.FileHeader
		file, header, err = r.FormFile("file")
		if err != nil {
			respondWithError(w, http.StatusBadRequest, "Failed to read file")
			return
		}
		defer file.Close()
		extracted, err = e.ExtractFromFile(file, header.Filename)
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

	pdfPath, err := pdfFormatter.FormatToPDF(extracted.Content, extracted.Metadata)
	if err != nil {
		log.Printf("PDF formatting failed: %v", err)
		respondWithError(w, http.StatusInternalServerError, "Failed to format content")
		return
	}

	// Read the PDF file
	pdfContent, err := os.ReadFile(pdfPath)
	if err != nil {
		log.Printf("Failed to read PDF file: %v", err)
		respondWithError(w, http.StatusInternalServerError, "Failed to read PDF file")
		return
	}

	// Send to Kindle
	mailer := mailer.NewMailer(cfg.KindleEmail, cfg.SenderEmail, cfg.SenderPassword)
	if err = mailer.SendToKindle(pdfContent); err != nil {
		log.Printf("Failed to send to Kindle: %v", err)
		respondWithError(w, http.StatusInternalServerError, "Failed to send to Kindle")
		return
	}

	// Clean up the temporary PDF file
	if err = os.Remove(pdfPath); err != nil {
		log.Printf("Failed to clean up PDF file: %v", err)
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
