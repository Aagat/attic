package api

import (
	"encoding/json"
	"errors"
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

// AddToKindleHandler handles requests to convert and send content to Kindle
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

	// Initialize components
	puppeteer, err := integrations.NewPuppeteer()
	if err != nil {
		log.Error().Err(err).Msg("Failed to initialize puppeteer")
		respondWithError(w, http.StatusInternalServerError, "Failed to initialize puppeteer")
		return
	}

	llmClient, err := llm.NewGeminiClient(llm.Config{
		APIKey: cfg.LLMAPIKey,
		Model:  cfg.Model,
	})
	if err != nil {
		log.Error().Err(err).Msg("Failed to initialize LLM client")
		respondWithError(w, http.StatusInternalServerError, "Failed to initialize LLM client")
		return
	}

	contentExtractor := extractor.NewExtractor(puppeteer, llmClient, cfg.StoragePath)
	pdfFormatter, err := formatter.NewFormatter(cfg.StoragePath)
	if err != nil {
		log.Error().Err(err).Msg("Failed to initialize formatter")
		respondWithError(w, http.StatusInternalServerError, "Failed to initialize formatter")
		return
	}

	// Parse multipart form
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		log.Error().Err(err).Msg("Failed to parse form")
		respondWithError(w, http.StatusBadRequest, "Failed to parse form")
		return
	}

	// Extract content
	var extracted *extractor.ExtractedContent
	if url := r.FormValue("url"); url != "" {
		log.Info().Str("url", url).Msg("Processing URL")
		extracted, err = contentExtractor.ExtractFromURL(url)
	} else {
		var file multipart.File
		var header *multipart.FileHeader
		file, header, err = r.FormFile("file")
		if err != nil {
			log.Error().Err(err).Msg("Failed to get file")
			respondWithError(w, http.StatusBadRequest, "Failed to get file")
			return
		}
		defer file.Close()
		log.Info().Str("filename", header.Filename).Msg("Processing file")
		extracted, err = contentExtractor.ExtractFromFile(file, header.Filename)
	}

	if err != nil {
		log.Error().Err(err).Msg("Failed to extract content")
		var extractErr *extractor.ExtractError
		if errors.As(err, &extractErr) {
			// Return the detailed error message for known error types
			if errors.Is(extractErr.Type, extractor.ErrNonArticle) ||
				errors.Is(extractErr.Type, extractor.ErrPaywall) ||
				errors.Is(extractErr.Type, extractor.ErrPartialContent) {
				respondWithError(w, http.StatusUnprocessableEntity, extractErr.Error())
				return
			}
		}
		respondWithError(w, http.StatusInternalServerError, "Failed to extract content")
		return
	}

	// Convert to PDF
	log.Info().Msg("Converting content to PDF")
	pdfPath, err := pdfFormatter.FormatToPDF(extracted.Content, extracted.Metadata)
	if err != nil {
		log.Error().Err(err).Msg("Failed to convert to PDF")
		respondWithError(w, http.StatusInternalServerError, "Failed to convert to PDF")
		return
	}

	// Send to Kindle if email is enabled
	if cfg.EmailEnabled {
		log.Info().Msg("Sending to Kindle")
		pdfContent, err := os.ReadFile(pdfPath)
		if err != nil {
			log.Error().Err(err).Msg("Failed to read PDF file")
			respondWithError(w, http.StatusInternalServerError, "Failed to read PDF file")
			return
		}
		mailer := mailer.NewMailer(cfg.KindleEmail, cfg.SenderEmail, cfg.SenderPassword)
		if err := mailer.SendToKindle(pdfContent); err != nil {
			log.Error().Err(err).Msg("Failed to send to Kindle")
			respondWithError(w, http.StatusInternalServerError, "Failed to send to Kindle")
			return
		}
	}

	// Return success
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status": "success",
		"path":   pdfPath,
	})
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
