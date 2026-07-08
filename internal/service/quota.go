package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"agenticquota/internal/model"

	"cloud.google.com/go/compute/metadata"
	monitoring "cloud.google.com/go/monitoring/apiv3/v2"
	"cloud.google.com/go/monitoring/apiv3/v2/monitoringpb"
	"google.golang.org/api/iterator"
	"google.golang.org/genproto/googleapis/api/metric"
	"google.golang.org/genproto/googleapis/api/monitoredres"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ErrNotFound is returned when no quota data is found in the store.
var ErrNotFound = errors.New("no quota data found")

// QuotaService provides operations on quotas.
type QuotaService interface {
	GetQuota(ctx context.Context) (model.QuotaResponse, error)
	SaveQuota(ctx context.Context, quota model.QuotaResponse) error
}

type gcpClient interface {
	createTimeSeries(ctx context.Context, req *monitoringpb.CreateTimeSeriesRequest) error
	listTimeSeries(ctx context.Context, req *monitoringpb.ListTimeSeriesRequest) ([]*monitoringpb.TimeSeries, error)
}

type realGCPClient struct {
	client *monitoring.MetricClient
}

func (c *realGCPClient) createTimeSeries(ctx context.Context, req *monitoringpb.CreateTimeSeriesRequest) error {
	return c.client.CreateTimeSeries(ctx, req)
}

func (c *realGCPClient) listTimeSeries(ctx context.Context, req *monitoringpb.ListTimeSeriesRequest) ([]*monitoringpb.TimeSeries, error) {
	it := c.client.ListTimeSeries(ctx, req)
	var list []*monitoringpb.TimeSeries
	for {
		resp, err := it.Next()
		if err != nil {
			if errors.Is(err, iterator.Done) {
				break
			}
			return nil, err
		}
		list = append(list, resp)
	}
	return list, nil
}

type quotaService struct {
	mu         sync.RWMutex
	lastQuota  model.QuotaResponse
	hasRecords bool

	gcpEnabled bool
	projectID  string
	client     gcpClient
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
		return &quotaService{gcpEnabled: false}
	}

	ctx := context.Background()
	client, err := monitoring.NewMetricClient(ctx)
	if err != nil {
		log.Printf("Failed to initialize GCP Monitoring Client: %v. Falling back to in-memory quota store.", err)
		return &quotaService{gcpEnabled: false}
	}

	log.Printf("Successfully initialized GCP Monitoring Client for project: %s", projectID)
	return &quotaService{
		gcpEnabled: true,
		projectID:  projectID,
		client:     &realGCPClient{client: client},
	}
}

// GetQuota returns the last saved quota details.
func (s *quotaService) GetQuota(ctx context.Context) (model.QuotaResponse, error) {
	if !s.gcpEnabled {
		s.mu.RLock()
		defer s.mu.RUnlock()

		if !s.hasRecords {
			return model.QuotaResponse{}, ErrNotFound
		}
		return s.lastQuota, nil
	}

	fractions, err := s.listMetric(ctx, "custom.googleapis.com/quota/remaining_fraction")
	if err != nil {
		return model.QuotaResponse{}, fmt.Errorf("failed to retrieve remaining fraction metric: %w", err)
	}
	if len(fractions) == 0 {
		return model.QuotaResponse{}, ErrNotFound
	}

	resets, err := s.listMetric(ctx, "custom.googleapis.com/quota/reset_in_seconds")
	if err != nil {
		return model.QuotaResponse{}, fmt.Errorf("failed to retrieve reset in seconds metric: %w", err)
	}

	times, err := s.listMetric(ctx, "custom.googleapis.com/quota/reset_time_epoch")
	if err != nil {
		return model.QuotaResponse{}, fmt.Errorf("failed to retrieve reset time epoch metric: %w", err)
	}

	quotaMap := make(map[string]model.QuotaDetails)
	for name, fracVal := range fractions {
		fraction, ok := fracVal.(float64)
		if !ok {
			continue
		}

		var resetInSecs int64
		if val, ok := resets[name].(int64); ok {
			resetInSecs = val
		}

		var resetTime time.Time
		if val, ok := times[name].(int64); ok {
			resetTime = time.Unix(val, 0).UTC()
		}

		quotaMap[name] = model.QuotaDetails{
			RemainingFraction: fraction,
			ResetTime:         resetTime,
			ResetInSeconds:    resetInSecs,
		}
	}

	return model.QuotaResponse{Quota: quotaMap}, nil
}

// SaveQuota stores a new quota report.
func (s *quotaService) SaveQuota(ctx context.Context, quota model.QuotaResponse) error {
	if !s.gcpEnabled {
		s.mu.Lock()
		defer s.mu.Unlock()

		s.lastQuota = quota
		s.hasRecords = true
		return nil
	}

	now := time.Now()
	var timeSeries []*monitoringpb.TimeSeries

	for name, details := range quota.Quota {
		tsFrac := makeTimeSeries("custom.googleapis.com/quota/remaining_fraction", name, details.RemainingFraction, now)
		tsInSecs := makeTimeSeries("custom.googleapis.com/quota/reset_in_seconds", name, details.ResetInSeconds, now)
		tsTime := makeTimeSeries("custom.googleapis.com/quota/reset_time_epoch", name, details.ResetTime.Unix(), now)
		timeSeries = append(timeSeries, tsFrac, tsInSecs, tsTime)
	}

	if len(timeSeries) == 0 {
		return nil
	}

	req := &monitoringpb.CreateTimeSeriesRequest{
		Name:       "projects/" + s.projectID,
		TimeSeries: timeSeries,
	}

	err := s.client.createTimeSeries(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to write time series to GCP Monitoring: %w", err)
	}
	return nil
}

func (s *quotaService) listMetric(ctx context.Context, metricType string) (map[string]interface{}, error) {
	now := time.Now()
	startTime := now.Add(-24 * time.Hour)

	req := &monitoringpb.ListTimeSeriesRequest{
		Name:   "projects/" + s.projectID,
		Filter: fmt.Sprintf(`metric.type = "%s"`, metricType),
		Interval: &monitoringpb.TimeInterval{
			StartTime: timestamppb.New(startTime),
			EndTime:   timestamppb.New(now),
		},
		View: monitoringpb.ListTimeSeriesRequest_FULL,
	}

	timeSeriesList, err := s.client.listTimeSeries(ctx, req)
	if err != nil {
		return nil, err
	}

	results := make(map[string]interface{})
	for _, ts := range timeSeriesList {
		quotaName := ts.Metric.Labels["quota_name"]
		if quotaName == "" {
			continue
		}
		if len(ts.Points) == 0 {
			continue
		}
		// Find the latest point by checking the EndTime of the interval
		latestPoint := ts.Points[0]
		for _, p := range ts.Points {
			if p.Interval.EndTime.AsTime().After(latestPoint.Interval.EndTime.AsTime()) {
				latestPoint = p
			}
		}

		switch latestPoint.Value.Value.(type) {
		case *monitoringpb.TypedValue_DoubleValue:
			results[quotaName] = latestPoint.Value.GetDoubleValue()
		case *monitoringpb.TypedValue_Int64Value:
			results[quotaName] = latestPoint.Value.GetInt64Value()
		}
	}
	return results, nil
}

func makeTimeSeries(metricType string, quotaName string, value interface{}, now time.Time) *monitoringpb.TimeSeries {
	var typedValue *monitoringpb.TypedValue
	var valueType metric.MetricDescriptor_ValueType

	switch v := value.(type) {
	case float64:
		valueType = metric.MetricDescriptor_DOUBLE
		typedValue = &monitoringpb.TypedValue{
			Value: &monitoringpb.TypedValue_DoubleValue{
				DoubleValue: v,
			},
		}
	case int64:
		valueType = metric.MetricDescriptor_INT64
		typedValue = &monitoringpb.TypedValue{
			Value: &monitoringpb.TypedValue_Int64Value{
				Int64Value: v,
			},
		}
	}

	return &monitoringpb.TimeSeries{
		Metric: &metric.Metric{
			Type: metricType,
			Labels: map[string]string{
				"quota_name": quotaName,
			},
		},
		Resource: &monitoredres.MonitoredResource{
			Type: "global",
		},
		MetricKind: metric.MetricDescriptor_GAUGE,
		ValueType:  valueType,
		Points: []*monitoringpb.Point{
			{
				Interval: &monitoringpb.TimeInterval{
					StartTime: timestamppb.New(now),
					EndTime:   timestamppb.New(now),
				},
				Value: typedValue,
			},
		},
	}
}
