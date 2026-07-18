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
		store: &gcpStore{
			projectID: "test-project-123",
			client:    mockCli,
		},
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
			"100percent-quota": {
				RemainingFraction: 1.0,
				ResetTime:         time.Time{},
				ResetInSeconds:    0,
			},
		},
	}
	err = svc.SaveQuota(context.Background(), payload)
	if err != nil {
		t.Fatalf("SaveQuota failed: %v", err)
	}

	// Assert on the mockCli.timeSeries content: 2 metrics * 3 types + 1 metric * 1 type = 7 time series
	if len(mockCli.timeSeries) != 7 {
		t.Fatalf("expected 7 time series written, got %d", len(mockCli.timeSeries))
	}

	// 3. GetQuota -> expect it to retrieve the saved data from mockCli
	res, err := svc.GetQuota(context.Background())
	if err != nil {
		t.Fatalf("GetQuota failed: %v", err)
	}

	if len(res.Quota) != 3 {
		t.Fatalf("expected 3 quotas in response, got %d", len(res.Quota))
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

	details3, ok := res.Quota["100percent-quota"]
	if !ok {
		t.Fatal("expected key '100percent-quota' in retrieved quota")
	}
	if details3.RemainingFraction != 1.0 {
		t.Errorf("expected remaining fraction 1.0, got %f", details3.RemainingFraction)
	}
	if !details3.ResetTime.IsZero() {
		t.Errorf("expected reset time to be zero, got %v", details3.ResetTime)
	}
	if details3.ResetInSeconds != 0 {
		t.Errorf("expected reset in seconds to be 0, got %d", details3.ResetInSeconds)
	}
}

func TestQuotaService_GCP_Errors(t *testing.T) {
	mockCli := &mockGCPClient{
		createErr: errors.New("monitoring create failed"),
		listErr:   errors.New("monitoring list failed"),
	}
	svc := &quotaService{
		store: &gcpStore{
			projectID: "test-project-123",
			client:    mockCli,
		},
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

func TestQuotaService_GetQuotaResetHistory_Memory(t *testing.T) {
	svc := NewQuotaService()

	// 1. Save mock quota
	resetTime := time.Date(2026, 7, 8, 10, 0, 52, 0, time.UTC)
	mockQuota := model.QuotaResponse{
		Quota: map[string]model.QuotaDetails{
			"3p-5h": {
				RemainingFraction: 0.75,
				ResetTime:         resetTime,
				ResetInSeconds:    18000,
			},
		},
	}
	err := svc.SaveQuota(context.Background(), mockQuota)
	if err != nil {
		t.Fatalf("unexpected error on save: %v", err)
	}

	// 2. Fetch reset history
	res, err := svc.GetQuotaResetHistory(context.Background(), 1)
	if err != nil {
		t.Fatalf("unexpected error on get reset history: %v", err)
	}

	points, ok := res.History["3p-5h"]
	if !ok || len(points) == 0 {
		t.Fatal("expected reset history for key '3p-5h'")
	}

	if !points[0].ResetTime.Equal(resetTime) {
		t.Errorf("expected reset time %v, got %v", resetTime, points[0].ResetTime)
	}
}

func TestQuotaService_GetQuotaResetHistory_GCP(t *testing.T) {
	mockCli := &mockGCPClient{}
	svc := &quotaService{
		store: &gcpStore{
			projectID: "test-project-123",
			client:    mockCli,
		},
	}

	// Save historical quota to populate GCP mock client
	resetTime := time.Date(2026, 7, 8, 10, 0, 52, 0, time.UTC)
	payload := model.QuotaResponse{
		Quota: map[string]model.QuotaDetails{
			"3p-5h": {
				RemainingFraction: 0.85,
				ResetTime:         resetTime,
				ResetInSeconds:    17999,
			},
		},
	}
	err := svc.SaveQuota(context.Background(), payload)
	if err != nil {
		t.Fatalf("SaveQuota failed: %v", err)
	}

	// Fetch reset history
	res, err := svc.GetQuotaResetHistory(context.Background(), 1)
	if err != nil {
		t.Fatalf("GetQuotaResetHistory failed: %v", err)
	}

	points, ok := res.History["3p-5h"]
	if !ok || len(points) == 0 {
		t.Fatal("expected reset history for key '3p-5h'")
	}

	if !points[0].ResetTime.Equal(resetTime) {
		t.Errorf("expected reset time %v, got %v", resetTime, points[0].ResetTime)
	}
}

func TestQuotaService_GetQuotaResetHistory_Deduplication(t *testing.T) {
	svc := NewQuotaService()

	resetTime1 := time.Date(2026, 7, 8, 10, 0, 52, 0, time.UTC)
	resetTime2 := time.Date(2026, 7, 8, 11, 0, 52, 0, time.UTC)

	// Save quota with resetTime1
	payload1 := model.QuotaResponse{
		Quota: map[string]model.QuotaDetails{
			"3p-5h": {
				RemainingFraction: 0.9,
				ResetTime:         resetTime1,
				ResetInSeconds:    7200,
			},
		},
	}
	if err := svc.SaveQuota(context.Background(), payload1); err != nil {
		t.Fatal(err)
	}

	// Save same quota again with resetTime1 (duplicate)
	payload2 := model.QuotaResponse{
		Quota: map[string]model.QuotaDetails{
			"3p-5h": {
				RemainingFraction: 0.8,
				ResetTime:         resetTime1,
				ResetInSeconds:    3600,
			},
		},
	}
	if err := svc.SaveQuota(context.Background(), payload2); err != nil {
		t.Fatal(err)
	}

	// Save quota with resetTime2 (new reset time)
	payload3 := model.QuotaResponse{
		Quota: map[string]model.QuotaDetails{
			"3p-5h": {
				RemainingFraction: 0.7,
				ResetTime:         resetTime2,
				ResetInSeconds:    7200,
			},
		},
	}
	if err := svc.SaveQuota(context.Background(), payload3); err != nil {
		t.Fatal(err)
	}

	res, err := svc.GetQuotaResetHistory(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}

	points, ok := res.History["3p-5h"]
	if !ok {
		t.Fatal("expected reset history for key '3p-5h'")
	}

	// Should contain exactly 2 unique reset times (resetTime1 and resetTime2)
	if len(points) != 2 {
		t.Fatalf("expected exactly 2 points after deduplication, got: %d", len(points))
	}

	if !points[0].ResetTime.Equal(resetTime1) {
		t.Errorf("expected points[0] to be %v, got %v", resetTime1, points[0].ResetTime)
	}
	if !points[1].ResetTime.Equal(resetTime2) {
		t.Errorf("expected points[1] to be %v, got %v", resetTime2, points[1].ResetTime)
	}
}
