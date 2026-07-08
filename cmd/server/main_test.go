package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSetupRouter_HealthCheck(t *testing.T) {
	mux := setupRouter()

	req, err := http.NewRequest("GET", "/_ah/health", nil)
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200 OK, got: %d", rr.Code)
	}

	expected := "ok"
	if rr.Body.String() != expected {
		t.Errorf("expected body %q, got: %q", expected, rr.Body.String())
	}
}

func TestSetupRouter_QuotaEndpointProtected(t *testing.T) {
	mux := setupRouter()

	req, err := http.NewRequest("GET", "/api/v1/quota", nil)
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401 Unauthorized, got: %d", rr.Code)
	}
}
