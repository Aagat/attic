package api

import (
	"encoding/json"
	"mime/multipart"
	"net/http"
	"os"

	"github.com/aagat/attic/backend/config"
	"github.com/aagat/attic/backend/extractor"
	"github.com/aagat/attic/backend/formatter"
	"github.com/aagat/attic/backend/integrations"
	"github.com/aagat/attic/backend/integrations/llm"
	"github.com/aagat/attic/backend/logger"
	"github.com/aagat/attic/backend/mailer"
)

// AddToKindleHandler processes requests to convert and send content to Kindle
func AddToKindleHandler(w http.ResponseWriter, r *http.Request) {
	var err error
	log := logger.FromContext(r.Context())

	// Get configuration
	cfg, err := config.Load()
	if err != nil {
		log.Error().Err(err).Msg("Failed to load configuration")
		respondWithError(w, http.StatusInternalServerError, "Failed to load configuration")
		return
	}

	// Initialize LLM client
	llmClient, err := llm.NewGeminiClient(llm.Config{
		APIKey: cfg.LLMAPIKey,
		Model:  cfg.Model,
	})
	if err != nil {
		log.Error().Err(err).Msg("Failed to initialize LLM client")
		respondWithError(w, http.StatusInternalServerError, "Failed to initialize LLM client")
		return
	}

	// Initialize Puppeteer
	puppeteer, err := integrations.NewPuppeteer()
	if err != nil {
		log.Error().Err(err).Msg("Failed to initialize Puppeteer")
		respondWithError(w, http.StatusInternalServerError, "Failed to initialize Puppeteer")
		return
	}

	// Initialize extractor
	e := extractor.NewExtractor(puppeteer, llmClient)

	// Parse multipart form
	if err = r.ParseMultipartForm(32 << 20); err != nil {
		log.Error().Err(err).Msg("Invalid request format")
		respondWithError(w, http.StatusBadRequest, "Invalid request format")
		return
	}

	// Get request type
	requestType := r.FormValue("type")
	if requestType != "url" && requestType != "file" {
		log.Error().Str("type", requestType).Msg("Invalid request type")
		respondWithError(w, http.StatusBadRequest, "Invalid request type")
		return
	}

	var extracted *extractor.ExtractedContent

	// Extract content based on type
	if requestType == "url" {
		url := r.FormValue("url")
		log.Info().Str("url", url).Msg("Processing URL")
		extracted, err = e.ExtractFromURL(url)
	} else {
		var file multipart.File
		var header *multipart.FileHeader
		file, header, err = r.FormFile("file")
		if err != nil {
			log.Error().Err(err).Msg("Failed to read file")
			respondWithError(w, http.StatusBadRequest, "Failed to read file")
			return
		}
		defer file.Close()
		log.Info().Str("filename", header.Filename).Msg("Processing file")
		extracted, err = e.ExtractFromFile(file, header.Filename)
	}

	if err != nil {
		log.Error().Err(err).Msg("Content extraction failed")
		respondWithError(w, http.StatusInternalServerError, "Failed to extract content")
		return
	}

	// Format content to PDF
	pdfFormatter, err := formatter.NewFormatter(cfg.StoragePath)
	if err != nil {
		log.Error().Err(err).Msg("Failed to initialize formatter")
		respondWithError(w, http.StatusInternalServerError, "Failed to initialize formatter")
		return
	}

	log.Info().Msg("Converting content to PDF")
	pdfPath, err := pdfFormatter.FormatToPDF(extracted.Content, extracted.Metadata)
	if err != nil {
		log.Error().Err(err).Msg("PDF formatting failed")
		respondWithError(w, http.StatusInternalServerError, "Failed to format content")
		return
	}

	// Read the PDF file to verify it's valid
	if _, err := os.ReadFile(pdfPath); err != nil {
		log.Error().Err(err).Str("path", pdfPath).Msg("Failed to read PDF file")
		// We only delete files that failed to generate properly
		if err := os.Remove(pdfPath); err != nil {
			log.Error().Err(err).Str("path", pdfPath).Msg("Failed to clean up invalid PDF file")
		}
		respondWithError(w, http.StatusInternalServerError, "Failed to read PDF file")
		return
	}

	log.Info().Str("path", pdfPath).Msg("PDF generated successfully")

	// Send to Kindle if email is enabled
	if cfg.EmailEnabled {
		log.Info().Str("email", cfg.KindleEmail).Msg("Email delivery enabled, sending to Kindle")
		pdfContent, err := os.ReadFile(pdfPath)
		if err != nil {
			log.Error().Err(err).Str("path", pdfPath).Msg("Failed to read PDF file for email")
			respondWithError(w, http.StatusInternalServerError, "Failed to read PDF file")
			return
		}

		mailer := mailer.NewMailer(cfg.KindleEmail, cfg.SenderEmail, cfg.SenderPassword)
		if err = mailer.SendToKindle(pdfContent); err != nil {
			log.Error().Err(err).Str("email", cfg.KindleEmail).Msg("Failed to send to Kindle")
			respondWithError(w, http.StatusInternalServerError, "Failed to send to Kindle")
			return
		}
		log.Info().Str("email", cfg.KindleEmail).Msg("Successfully delivered to Kindle")
	}

	// Respond with success and file path
	response := map[string]string{
		"status":   "success",
		"message":  "PDF generated successfully",
		"pdf_path": pdfPath,
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
