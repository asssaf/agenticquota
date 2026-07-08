package service

import (
	"errors"
	"testing"
	"time"

	"agenticquota/internal/model"
)

func TestQuotaService_GetAndSave(t *testing.T) {
	svc := NewQuotaService()

	// 1. Getting quota before any save should return ErrNotFound
	_, err := svc.GetQuota()
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got: %v", err)
	}

	// 2. Save a mock quota
	mockQuota := model.QuotaResponse{
		Quota: map[string]model.QuotaDetails{
			"3p-5h": {
				RemainingFraction: 0.75,
				ResetTime:         time.Now().Add(5 * time.Hour),
				ResetInSeconds:    18000,
			},
		},
	}
	err = svc.SaveQuota(mockQuota)
	if err != nil {
		t.Fatalf("unexpected error on save: %v", err)
	}

	// 3. Getting quota should now return the saved quota
	res, err := svc.GetQuota()
	if err != nil {
		t.Fatalf("unexpected error on get: %v", err)
	}

	details, ok := res.Quota["3p-5h"]
	if !ok {
		t.Fatal("expected key '3p-5h' in retrieved quota")
	}

	if details.RemainingFraction != 0.75 {
		t.Errorf("expected remaining fraction 0.75, got: %f", details.RemainingFraction)
	}
}
