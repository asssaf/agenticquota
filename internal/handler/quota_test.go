package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"agenticquota/internal/model"
	"agenticquota/internal/service"
)

func TestQuotaHandler_APIKeyMiddleware_Unauthorized(t *testing.T) {
	svc := service.NewQuotaService()
	h := NewQuotaHandler(svc)

	req, err := http.NewRequest("GET", "/api/v1/quota", nil)
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	handler := APIKeyMiddleware(http.HandlerFunc(h.HandleQuota))
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401 Unauthorized, got: %d", rr.Code)
	}
}

func TestQuotaHandler_GetQuota_NotFound(t *testing.T) {
	t.Setenv("QUOTA_API_KEY", "testkey")
	svc := service.NewQuotaService()
	h := NewQuotaHandler(svc)

	req, err := http.NewRequest("GET", "/api/v1/quota", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-API-Key", "testkey")

	rr := httptest.NewRecorder()
	handler := APIKeyMiddleware(http.HandlerFunc(h.HandleQuota))
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected status 404 Not Found, got: %d", rr.Code)
	}
}

func TestQuotaHandler_PostAndGet_Success(t *testing.T) {
	t.Setenv("QUOTA_API_KEY", "testkey")
	svc := service.NewQuotaService()
	h := NewQuotaHandler(svc)

	// 1. Post a quota payload
	payload := model.QuotaResponse{
		Quota: map[string]model.QuotaDetails{
			"3p-5h": {
				RemainingFraction: 0.9,
				ResetTime:         time.Date(2026, 7, 8, 10, 0, 52, 0, time.UTC),
				ResetInSeconds:    17999,
			},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}

	reqPost, err := http.NewRequest("POST", "/api/v1/quota", bytes.NewBuffer(body))
	if err != nil {
		t.Fatal(err)
	}
	reqPost.Header.Set("X-API-Key", "testkey")
	reqPost.Header.Set("Content-Type", "application/json")

	rrPost := httptest.NewRecorder()
	handler := APIKeyMiddleware(http.HandlerFunc(h.HandleQuota))
	handler.ServeHTTP(rrPost, reqPost)

	if rrPost.Code != http.StatusOK {
		t.Errorf("expected POST status 200 OK, got: %d", rrPost.Code)
	}

	// 2. Get the quota payload and verify it matches
	reqGet, err := http.NewRequest("GET", "/api/v1/quota", nil)
	if err != nil {
		t.Fatal(err)
	}
	reqGet.Header.Set("X-API-Key", "testkey")

	rrGet := httptest.NewRecorder()
	handler.ServeHTTP(rrGet, reqGet)

	if rrGet.Code != http.StatusOK {
		t.Errorf("expected GET status 200 OK, got: %d", rrGet.Code)
	}

	var resp model.QuotaResponse
	if err := json.NewDecoder(rrGet.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode JSON response: %v", err)
	}

	details, ok := resp.Quota["3p-5h"]
	if !ok {
		t.Fatal("expected '3p-5h' key in quota response")
	}
	if details.RemainingFraction != 0.9 {
		t.Errorf("expected remaining_fraction 0.9, got: %f", details.RemainingFraction)
	}
}

func TestQuotaHandler_GetQuota_MethodNotAllowed(t *testing.T) {
	t.Setenv("QUOTA_API_KEY", "testkey")
	svc := service.NewQuotaService()
	h := NewQuotaHandler(svc)

	methods := []string{"PUT", "DELETE"}
	for _, method := range methods {
		req, err := http.NewRequest(method, "/api/v1/quota", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("X-API-Key", "testkey")

		rr := httptest.NewRecorder()
		handler := APIKeyMiddleware(http.HandlerFunc(h.HandleQuota))
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusMethodNotAllowed {
			t.Errorf("expected status 405 Method Not Allowed for %s, got: %d", method, rr.Code)
		}
	}
}

func TestQuotaHandler_GetQuota_DefaultKeyFallback(t *testing.T) {
	t.Setenv("QUOTA_API_KEY", "")

	svc := service.NewQuotaService()
	h := NewQuotaHandler(svc)

	reqSuccess, err := http.NewRequest("GET", "/api/v1/quota", nil)
	if err != nil {
		t.Fatal(err)
	}
	reqSuccess.Header.Set("X-API-Key", "default-secret-key")

	rrSuccess := httptest.NewRecorder()
	handler := APIKeyMiddleware(http.HandlerFunc(h.HandleQuota))
	handler.ServeHTTP(rrSuccess, reqSuccess)

	// Since database is empty, it should bypass authentication but return 404
	if rrSuccess.Code != http.StatusNotFound {
		t.Errorf("expected fallback authentication success followed by status 404, got: %d", rrSuccess.Code)
	}
}

func TestQuotaHandler_GetQuotaHistory_Success(t *testing.T) {
	t.Setenv("QUOTA_API_KEY", "testkey")
	svc := service.NewQuotaService()
	h := NewQuotaHandler(svc)

	// 1. Post a quota payload to populate history
	payload := model.QuotaResponse{
		Quota: map[string]model.QuotaDetails{
			"3p-5h": {
				RemainingFraction: 0.9,
				ResetTime:         time.Now().Add(2 * time.Hour),
				ResetInSeconds:    7200,
			},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}

	reqPost, err := http.NewRequest("POST", "/api/v1/quota", bytes.NewBuffer(body))
	if err != nil {
		t.Fatal(err)
	}
	reqPost.Header.Set("X-API-Key", "testkey")
	reqPost.Header.Set("Content-Type", "application/json")

	rrPost := httptest.NewRecorder()
	h.HandleQuota(rrPost, reqPost)

	if rrPost.Code != http.StatusOK {
		t.Errorf("expected POST status 200 OK, got: %d", rrPost.Code)
	}

	// 2. Fetch history with default timeframe (days=1)
	reqGet1, err := http.NewRequest("GET", "/api/v1/quota/history?days=1", nil)
	if err != nil {
		t.Fatal(err)
	}
	reqGet1.Header.Set("X-API-Key", "testkey")

	rrGet1 := httptest.NewRecorder()
	h.HandleQuotaHistory(rrGet1, reqGet1)

	if rrGet1.Code != http.StatusOK {
		t.Errorf("expected status 200 OK for history (days=1), got: %d", rrGet1.Code)
	}

	var resp1 model.QuotaHistoryResponse
	if err := json.NewDecoder(rrGet1.Body).Decode(&resp1); err != nil {
		t.Fatalf("failed to decode JSON response: %v", err)
	}

	points1, ok := resp1.History["3p-5h"]
	if !ok || len(points1) == 0 {
		t.Fatal("expected history data for key '3p-5h'")
	}
	if points1[0].Value != 0.9 {
		t.Errorf("expected value 0.9, got: %f", points1[0].Value)
	}

	// 3. Fetch history with 7-day timeframe (days=7)
	reqGet7, err := http.NewRequest("GET", "/api/v1/quota/history?days=7", nil)
	if err != nil {
		t.Fatal(err)
	}
	reqGet7.Header.Set("X-API-Key", "testkey")

	rrGet7 := httptest.NewRecorder()
	h.HandleQuotaHistory(rrGet7, reqGet7)

	if rrGet7.Code != http.StatusOK {
		t.Errorf("expected status 200 OK for history (days=7), got: %d", rrGet7.Code)
	}
}

func TestQuotaHandler_GetQuotaResetHistory_Success(t *testing.T) {
	t.Setenv("QUOTA_API_KEY", "testkey")
	svc := service.NewQuotaService()
	h := NewQuotaHandler(svc)

	// 1. Post a quota payload to populate history
	resetTime := time.Date(2026, 7, 8, 10, 0, 52, 0, time.UTC)
	payload := model.QuotaResponse{
		Quota: map[string]model.QuotaDetails{
			"3p-5h": {
				RemainingFraction: 0.9,
				ResetTime:         resetTime,
				ResetInSeconds:    7200,
			},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}

	reqPost, err := http.NewRequest("POST", "/api/v1/quota", bytes.NewBuffer(body))
	if err != nil {
		t.Fatal(err)
	}
	reqPost.Header.Set("X-API-Key", "testkey")
	reqPost.Header.Set("Content-Type", "application/json")

	rrPost := httptest.NewRecorder()
	h.HandleQuota(rrPost, reqPost)

	if rrPost.Code != http.StatusOK {
		t.Errorf("expected POST status 200 OK, got: %d", rrPost.Code)
	}

	// 2. Fetch reset history with default timeframe (days=1)
	reqGet1, err := http.NewRequest("GET", "/api/v1/quota/history/reset?days=1", nil)
	if err != nil {
		t.Fatal(err)
	}
	reqGet1.Header.Set("X-API-Key", "testkey")

	rrGet1 := httptest.NewRecorder()
	h.HandleQuotaResetHistory(rrGet1, reqGet1)

	if rrGet1.Code != http.StatusOK {
		t.Errorf("expected status 200 OK for reset history (days=1), got: %d", rrGet1.Code)
	}

	var resp1 model.QuotaResetHistoryResponse
	if err := json.NewDecoder(rrGet1.Body).Decode(&resp1); err != nil {
		t.Fatalf("failed to decode JSON response: %v", err)
	}

	points1, ok := resp1.History["3p-5h"]
	if !ok || len(points1) == 0 {
		t.Fatal("expected reset history data for key '3p-5h'")
	}
	if !points1[0].ResetTime.Equal(resetTime) {
		t.Errorf("expected reset time %v, got: %v", resetTime, points1[0].ResetTime)
	}
}
