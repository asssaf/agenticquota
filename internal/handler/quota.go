package handler

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"

	"agenticquota/internal/model"
	"agenticquota/internal/service"
)

// QuotaHandler handles requests to report and retrieve quotas.
type QuotaHandler struct {
	service service.QuotaService
}

// NewQuotaHandler creates a new instance of QuotaHandler.
func NewQuotaHandler(s service.QuotaService) *QuotaHandler {
	return &QuotaHandler{service: s}
}

// APIKeyMiddleware validates the request has a valid X-API-Key header.
func APIKeyMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Retrieve API key from environment variable
		expectedKey := os.Getenv("QUOTA_API_KEY")
		if expectedKey == "" {
			// Fallback/Default for local development if not configured
			expectedKey = "default-secret-key"
		}

		apiKey := r.Header.Get("X-API-Key")
		if apiKey == "" || apiKey != expectedKey {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{
				"error": "unauthorized: invalid or missing API key",
			})
			return
		}

		next.ServeHTTP(w, r)
	})
}

// HandleQuota routes and processes GET and POST requests for /api/v1/quota.
func (h *QuotaHandler) HandleQuota(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case http.MethodGet:
		h.getQuota(w, r)
	case http.MethodPost:
		h.postQuota(w, r)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "method not allowed",
		})
	}
}

func (h *QuotaHandler) getQuota(w http.ResponseWriter, r *http.Request) {
	response, err := h.service.GetQuota(r.Context())
	if err != nil {
		if errors.Is(err, service.ErrNotFound) {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{
				"error": "no quota data found",
			})
			return
		}
		log.Printf("Error retrieving quota: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "failed to retrieve quota information",
		})
		return
	}

	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (h *QuotaHandler) postQuota(w http.ResponseWriter, r *http.Request) {
	var req model.QuotaResponse
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "invalid request body or JSON format",
		})
		return
	}

	if err := h.service.SaveQuota(r.Context(), req); err != nil {
		log.Printf("Error saving quota: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "failed to save quota data",
		})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "success",
		"message": "quota updated successfully",
	})
}
