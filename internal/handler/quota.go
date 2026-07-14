package handler

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

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

	// Validate quota details
	for _, details := range req.Quota {
		if details.ResetTime.Before(time.Now()) {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{
				"error": "invalid quota: reset time is in the past",
			})
			return
		}
		if details.ResetInSeconds <= 0 {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{
				"error": "invalid quota: remaining seconds must be positive",
			})
			return
		}
		if details.RemainingFraction < 0 || details.RemainingFraction > 1 {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{
				"error": "invalid quota: remaining fraction must be between 0 and 1",
			})
			return
		}
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

// HandleQuotaHistory routes and processes GET requests for /api/v1/quota/history.
func (h *QuotaHandler) HandleQuotaHistory(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "method not allowed",
		})
		return
	}

	days := 1
	daysStr := r.URL.Query().Get("days")
	if daysStr != "" {
		if val, err := strconv.Atoi(daysStr); err == nil && (val == 1 || val == 7) {
			days = val
		}
	}

	response, err := h.service.GetQuotaHistory(r.Context(), days)
	if err != nil {
		log.Printf("Error retrieving quota history: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "failed to retrieve quota history",
		})
		return
	}

	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// HandleQuotaResetHistory routes and processes GET requests for /api/v1/quota/history/reset.
func (h *QuotaHandler) HandleQuotaResetHistory(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "method not allowed",
		})
		return
	}

	days := 1
	daysStr := r.URL.Query().Get("days")
	if daysStr != "" {
		if val, err := strconv.Atoi(daysStr); err == nil && (val == 1 || val == 7) {
			days = val
		}
	}

	response, err := h.service.GetQuotaResetHistory(r.Context(), days)
	if err != nil {
		log.Printf("Error retrieving quota reset history: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "failed to retrieve quota reset history",
		})
		return
	}

	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
