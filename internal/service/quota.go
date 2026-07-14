package service

import (
	"context"
	"errors"
	"flag"
	"log"
	"os"
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
		s := &inMemoryQuotaService{}
		if flag.Lookup("test.v") == nil {
			s.seedFakeData()
		}
		return s
	}

	ctx := context.Background()
	client, err := monitoring.NewMetricClient(ctx)
	if err != nil {
		log.Printf("Failed to initialize GCP Monitoring Client: %v. Falling back to in-memory quota store.", err)
		return &inMemoryQuotaService{}
	}

	log.Printf("Successfully initialized GCP Monitoring Client for project: %s", projectID)
	return &gcpQuotaService{
		projectID: projectID,
		client:    &realGCPClient{client: client},
	}
}
