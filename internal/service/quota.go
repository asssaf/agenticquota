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
)

// ErrNotFound is returned when no quota data is found in the store.
var ErrNotFound = errors.New("no quota data found")

// QuotaService provides operations on quotas.
type QuotaService interface {
	GetQuota(ctx context.Context) (model.QuotaResponse, error)
	GetQuotaHistory(ctx context.Context, days int) (model.QuotaHistoryResponse, error)
	SaveQuota(ctx context.Context, quota model.QuotaResponse) error
}

type cacheEntry struct {
	data       interface{}
	expiration time.Time
}

type quotaService struct {
	mu                sync.RWMutex
	store             QuotaStore
	quotaCache        *cacheEntry
	historyCache1Day  *cacheEntry
	historyCache7Days *cacheEntry
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

	if projectID == "" {
		log.Println("GOOGLE_CLOUD_PROJECT is empty. Falling back to in-memory quota store.")
		seed := flag.Lookup("test.v") == nil
		return &quotaService{
			store: newInMemoryStore(seed),
		}
	}

	ctx := context.Background()
	client, err := monitoring.NewMetricClient(ctx)
	if err != nil {
		log.Printf("Failed to initialize GCP Monitoring Client: %v. Falling back to in-memory quota store.", err)
		return &quotaService{
			store: newInMemoryStore(false),
		}
	}

	log.Printf("Successfully initialized GCP Monitoring Client for project: %s", projectID)
	return &quotaService{
		store: &gcpStore{
			projectID: projectID,
			client:    &realGCPClient{client: client},
		},
	}
}

// GetQuota returns the last saved quota details.
func (s *quotaService) GetQuota(ctx context.Context) (model.QuotaResponse, error) {
	if cached, ok := s.getCachedQuota(); ok {
		return cached, nil
	}

	response, err := s.store.GetQuota(ctx)
	if err != nil {
		return model.QuotaResponse{}, err
	}

	s.setCachedQuota(response)
	return response, nil
}

// SaveQuota stores a new quota report.
func (s *quotaService) SaveQuota(ctx context.Context, quota model.QuotaResponse) error {
	s.invalidateCache()
	return s.store.SaveQuota(ctx, quota)
}

// GetQuotaHistory retrieves the 24-hour or 7-day historical utilization series for all quotas.
func (s *quotaService) GetQuotaHistory(ctx context.Context, days int) (model.QuotaHistoryResponse, error) {
	if cached, ok := s.getCachedHistory(days); ok {
		return cached, nil
	}

	response, err := s.store.GetQuotaHistory(ctx, days)
	if err != nil {
		return model.QuotaHistoryResponse{}, err
	}

	s.setCachedHistory(days, response)
	return response, nil
}

func (s *quotaService) getCachedQuota() (model.QuotaResponse, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.quotaCache != nil && time.Now().Before(s.quotaCache.expiration) {
		return s.quotaCache.data.(model.QuotaResponse), true
	}
	return model.QuotaResponse{}, false
}

func (s *quotaService) setCachedQuota(response model.QuotaResponse) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.quotaCache = &cacheEntry{
		data:       response,
		expiration: time.Now().Add(30 * time.Second),
	}
}

func (s *quotaService) getCachedHistory(days int) (model.QuotaHistoryResponse, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if days == 1 && s.historyCache1Day != nil && time.Now().Before(s.historyCache1Day.expiration) {
		return s.historyCache1Day.data.(model.QuotaHistoryResponse), true
	}
	if days == 7 && s.historyCache7Days != nil && time.Now().Before(s.historyCache7Days.expiration) {
		return s.historyCache7Days.data.(model.QuotaHistoryResponse), true
	}
	return model.QuotaHistoryResponse{}, false
}

func (s *quotaService) setCachedHistory(days int, response model.QuotaHistoryResponse) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry := &cacheEntry{
		data:       response,
		expiration: time.Now().Add(30 * time.Second),
	}

	if days == 1 {
		s.historyCache1Day = entry
	} else if days == 7 {
		s.historyCache7Days = entry
	}
}

func (s *quotaService) invalidateCache() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.quotaCache = nil
	s.historyCache1Day = nil
	s.historyCache7Days = nil
}
