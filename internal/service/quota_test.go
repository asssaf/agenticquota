package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"agenticquota/internal/model"
	"cloud.google.com/go/monitoring/apiv3/v2/monitoringpb"
)

type mockGCPClient struct {
	timeSeries []*monitoringpb.TimeSeries
	createErr  error
	listErr    error
}

func (c *mockGCPClient) createTimeSeries(ctx context.Context, req *monitoringpb.CreateTimeSeriesRequest) error {
	if c.createErr != nil {
		return c.createErr
	}
	c.timeSeries = append(c.timeSeries, req.TimeSeries...)
	return nil
}

func (c *mockGCPClient) listTimeSeries(ctx context.Context, req *monitoringpb.ListTimeSeriesRequest) ([]*monitoringpb.TimeSeries, error) {
	if c.listErr != nil {
		return nil, c.listErr
	}
	var filtered []*monitoringpb.TimeSeries
	for _, ts := range c.timeSeries {
		if strings.Contains(req.Filter, ts.Metric.Type) {
			filtered = append(filtered, ts)
		}
	}
	return filtered, nil
}

func TestQuotaService_GetAndSave(t *testing.T) {
	svc := NewQuotaService()

	// 1. Getting quota before any save should return ErrNotFound
	_, err := svc.GetQuota(context.Background())
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got: %v", err)
	}

	// 2. Save a mock quota
	mockQuota := model.QuotaResponse{
		Quota: map[string]model.QuotaDetails{
			"3p-5h": {
				RemainingFraction: 0.75,
				ResetTime:         time.Now().Add(5 * time.Hour).Truncate(time.Second),
				ResetInSeconds:    18000,
			},
		},
	}
	err = svc.SaveQuota(context.Background(), mockQuota)
	if err != nil {
		t.Fatalf("unexpected error on save: %v", err)
	}

	// 3. Getting quota should now return the saved quota
	res, err := svc.GetQuota(context.Background())
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

func TestQuotaService_GCP(t *testing.T) {
	mockCli := &mockGCPClient{}
	svc := &quotaService{
		gcpEnabled: true,
		projectID:  "test-project-123",
		client:     mockCli,
	}

	// 1. GetQuota when empty -> expect ErrNotFound
	_, err := svc.GetQuota(context.Background())
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	// 2. SaveQuota with multiple quotas
	resetTime1 := time.Date(2026, 7, 8, 10, 0, 52, 0, time.UTC)
	resetTime2 := time.Date(2026, 7, 8, 12, 30, 0, 0, time.UTC)
	payload := model.QuotaResponse{
		Quota: map[string]model.QuotaDetails{
			"3p-5h": {
				RemainingFraction: 0.85,
				ResetTime:         resetTime1,
				ResetInSeconds:    17999,
			},
			"gemini-5h": {
				RemainingFraction: 0.50,
				ResetTime:         resetTime2,
				ResetInSeconds:    9000,
			},
		},
	}
	err = svc.SaveQuota(context.Background(), payload)
	if err != nil {
		t.Fatalf("SaveQuota failed: %v", err)
	}

	// Assert on the mockCli.timeSeries content: 2 metrics * 3 types = 6 time series
	if len(mockCli.timeSeries) != 6 {
		t.Fatalf("expected 6 time series written, got %d", len(mockCli.timeSeries))
	}

	// 3. GetQuota -> expect it to retrieve the saved data from mockCli
	res, err := svc.GetQuota(context.Background())
	if err != nil {
		t.Fatalf("GetQuota failed: %v", err)
	}

	if len(res.Quota) != 2 {
		t.Fatalf("expected 2 quotas in response, got %d", len(res.Quota))
	}

	details1, ok := res.Quota["3p-5h"]
	if !ok {
		t.Fatal("expected key '3p-5h' in retrieved quota")
	}
	if details1.RemainingFraction != 0.85 {
		t.Errorf("expected remaining fraction 0.85, got %f", details1.RemainingFraction)
	}
	if !details1.ResetTime.Equal(resetTime1) {
		t.Errorf("expected reset time %v, got %v", resetTime1, details1.ResetTime)
	}
	if details1.ResetInSeconds != 17999 {
		t.Errorf("expected reset in seconds 17999, got %d", details1.ResetInSeconds)
	}

	details2, ok := res.Quota["gemini-5h"]
	if !ok {
		t.Fatal("expected key 'gemini-5h' in retrieved quota")
	}
	if details2.RemainingFraction != 0.50 {
		t.Errorf("expected remaining fraction 0.50, got %f", details2.RemainingFraction)
	}
	if !details2.ResetTime.Equal(resetTime2) {
		t.Errorf("expected reset time %v, got %v", resetTime2, details2.ResetTime)
	}
	if details2.ResetInSeconds != 9000 {
		t.Errorf("expected reset in seconds 9000, got %d", details2.ResetInSeconds)
	}
}

func TestQuotaService_GCP_Errors(t *testing.T) {
	mockCli := &mockGCPClient{
		createErr: errors.New("monitoring create failed"),
		listErr:   errors.New("monitoring list failed"),
	}
	svc := &quotaService{
		gcpEnabled: true,
		projectID:  "test-project-123",
		client:     mockCli,
	}

	// 1. SaveQuota error
	payload := model.QuotaResponse{
		Quota: map[string]model.QuotaDetails{
			"3p-5h": {
				RemainingFraction: 0.85,
				ResetTime:         time.Now(),
				ResetInSeconds:    17999,
			},
		},
	}
	err := svc.SaveQuota(context.Background(), payload)
	if err == nil || !strings.Contains(err.Error(), "monitoring create failed") {
		t.Fatalf("expected create error, got %v", err)
	}

	// 2. GetQuota error
	_, err = svc.GetQuota(context.Background())
	if err == nil || !strings.Contains(err.Error(), "monitoring list failed") {
		t.Fatalf("expected list error, got %v", err)
	}
}
