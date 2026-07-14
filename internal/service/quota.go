package service

import (
	"context"
	"errors"
	"flag"
	"log"
	"os"
	"sync"
	"time"

	"agenticquota/internal/model"

	"cloud.google.com/go/compute/metadata"
	monitoring "cloud.google.com/go/monitoring/apiv3/v2"
	"cloud.google.com/go/monitoring/apiv3/v2/monitoringpb"
)

// ErrNotFound is returned when no quota data is found in the store.
var ErrNotFound = errors.New("no quota data found")

// QuotaService provides operations on quotas.
type QuotaService interface {
	GetQuota(ctx context.Context) (model.QuotaResponse, error)
	GetQuotaHistory(ctx context.Context, days int) (model.QuotaHistoryResponse, error)
	SaveQuota(ctx context.Context, quota model.QuotaResponse) error
}

type gcpClient interface {
	createTimeSeries(ctx context.Context, req *monitoringpb.CreateTimeSeriesRequest) error
	listTimeSeries(ctx context.Context, req *monitoringpb.ListTimeSeriesRequest) ([]*monitoringpb.TimeSeries, error)
}

type historicalRecord struct {
	Timestamp time.Time
	Quota     model.QuotaResponse
}

type cacheEntry struct {
	data       interface{}
	expiration time.Time
}

type QuotaReset struct {
	Name      string
	ResetTime time.Time
}

type QuotaStore interface {
	GetQuota(ctx context.Context) (model.QuotaResponse, error)
	GetQuotaHistory(ctx context.Context, days int) (model.QuotaHistoryResponse, error)
	GetPreviousQuota(ctx context.Context) (map[string]model.QuotaDetails, map[string]time.Time, error)
	SaveResetMetrics(ctx context.Context, resets []QuotaReset) error
	SaveQuotaMetrics(ctx context.Context, quota model.QuotaResponse, now time.Time) error
}

type quotaService struct {
	store QuotaStore
	mu    sync.Mutex
}

// NewQuotaService creates a new instance of QuotaService.
func NewQuotaService() QuotaService {
	projectID := os.Getenv("GOOGLE_CLOUD_PROJECT")
	if projectID == "" {
		projectID = os.Getenv("GCLOUD_PROJECT")
	}

	if projectID == "" && metadata.OnGCE() {
		if pid, err := metadata.ProjectID(); err == nil {
			projectID = pid
		}
	}

	var store QuotaStore
	if projectID == "" {
		log.Println("GOOGLE_CLOUD_PROJECT is empty. Falling back to in-memory quota store.")
		inMemStore := &inMemoryQuotaStore{}
		if flag.Lookup("test.v") == nil {
			inMemStore.seedFakeData()
		}
		store = inMemStore
	} else {
		ctx := context.Background()
		client, err := monitoring.NewMetricClient(ctx)
		if err != nil {
			log.Printf("Failed to initialize GCP Monitoring Client: %v. Falling back to in-memory quota store.", err)
			store = &inMemoryQuotaStore{}
		} else {
			log.Printf("Successfully initialized GCP Monitoring Client for project: %s", projectID)
			store = &gcpQuotaStore{
				projectID: projectID,
				client:    &realGCPClient{client: client},
			}
		}
	}

	return &quotaService{store: store}
}

func (s *quotaService) GetQuota(ctx context.Context) (model.QuotaResponse, error) {
	return s.store.GetQuota(ctx)
}

func (s *quotaService) GetQuotaHistory(ctx context.Context, days int) (model.QuotaHistoryResponse, error) {
	return s.store.GetQuotaHistory(ctx, days)
}

func (s *quotaService) SaveQuota(ctx context.Context, quota model.QuotaResponse) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 1. Fetch previous quota and timestamps
	prevQuotas, prevTimestamps, err := s.store.GetPreviousQuota(ctx)
	if err != nil {
		log.Printf("Could not retrieve previous quota (might be empty/error): %v", err)
	}

	now := time.Now().UTC()

	// 2. Check for resets and generate reset metrics if needed
	if prevQuotas != nil && prevTimestamps != nil {
		resets := DetectResets(prevQuotas, prevTimestamps, quota.Quota, now)
		if len(resets) > 0 {
			if err := s.store.SaveResetMetrics(ctx, resets); err != nil {
				log.Printf("Failed to save reset metrics: %v", err)
			}
		}
	}

	// 3. Save new quota metrics
	return s.store.SaveQuotaMetrics(ctx, quota, now)
}

func DetectResets(prev map[string]model.QuotaDetails, prevTimes map[string]time.Time, current map[string]model.QuotaDetails, now time.Time) []QuotaReset {
	var resets []QuotaReset
	for name := range current {
		tPrev, okT := prevTimes[name]
		prevDetails, okQ := prev[name]
		if okT && okQ {
			rtPrev := prevDetails.ResetTime
			if !rtPrev.IsZero() && rtPrev.After(tPrev) && rtPrev.Before(now) {
				resets = append(resets, QuotaReset{
					Name:      name,
					ResetTime: rtPrev,
				})
			}
		}
	}
	return resets
}
