package service

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"sort"
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
	GetQuotaHistory(ctx context.Context, days int) (model.QuotaHistoryResponse, error)
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

type historicalRecord struct {
	Timestamp time.Time
	Quota     model.QuotaResponse
}

type cacheEntry struct {
	data       interface{}
	expiration time.Time
}

type quotaService struct {
	mu         sync.RWMutex
	lastQuota  model.QuotaResponse
	hasRecords bool
	history    []historicalRecord

	gcpEnabled bool
	projectID  string
	client     gcpClient

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
		s := &quotaService{gcpEnabled: false}
		if flag.Lookup("test.v") == nil {
			s.seedFakeData()
		}
		return s
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

	if cached, ok := s.getCachedQuota(); ok {
		return cached, nil
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

	response := model.QuotaResponse{Quota: quotaMap}
	s.setCachedQuota(response)

	return response, nil
}

// SaveQuota stores a new quota report.
func (s *quotaService) SaveQuota(ctx context.Context, quota model.QuotaResponse) error {
	if !s.gcpEnabled {
		s.mu.Lock()
		defer s.mu.Unlock()

		s.lastQuota = quota
		s.hasRecords = true

		// Record history in-memory
		s.history = append(s.history, historicalRecord{
			Timestamp: time.Now().UTC(),
			Quota:     quota,
		})

		// Limit in-memory history size to prevent memory exhaustion (up to 7 days)
		const maxHistory = 3000
		if len(s.history) > maxHistory {
			s.history = s.history[len(s.history)-maxHistory:]
		}

		// Prune records older than 7 days
		cutoff := time.Now().Add(-7 * 24 * time.Hour)
		idx := 0
		for idx < len(s.history) && s.history[idx].Timestamp.Before(cutoff) {
			idx++
		}
		if idx > 0 {
			s.history = s.history[idx:]
		}

		return nil
	}

	s.invalidateCache()

	now := time.Now()
	var timeSeries []*monitoringpb.TimeSeries

	for name, details := range quota.Quota {
		tsFrac := makeTimeSeries(s.projectID, "custom.googleapis.com/quota/remaining_fraction", name, details.RemainingFraction, now)
		tsInSecs := makeTimeSeries(s.projectID, "custom.googleapis.com/quota/reset_in_seconds", name, details.ResetInSeconds, now)
		tsTime := makeTimeSeries(s.projectID, "custom.googleapis.com/quota/reset_time_epoch", name, details.ResetTime.Unix(), now)
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

func makeTimeSeries(projectID string, metricType string, quotaName string, value interface{}, now time.Time) *monitoringpb.TimeSeries {
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
			Labels: map[string]string{
				"project_id": projectID,
			},
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

// GetQuotaHistory retrieves the 24-hour or 7-day historical utilization series for all quotas.
func (s *quotaService) GetQuotaHistory(ctx context.Context, days int) (model.QuotaHistoryResponse, error) {
	if !s.gcpEnabled {
		s.mu.RLock()
		defer s.mu.RUnlock()

		cutoff := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
		historyMap := make(map[string][]model.HistoricalPoint)
		for _, record := range s.history {
			if record.Timestamp.Before(cutoff) {
				continue
			}
			for name, details := range record.Quota.Quota {
				historyMap[name] = append(historyMap[name], model.HistoricalPoint{
					Timestamp: record.Timestamp,
					Value:     details.RemainingFraction,
				})
			}
		}
		return model.QuotaHistoryResponse{History: historyMap}, nil
	}

	if cached, ok := s.getCachedHistory(days); ok {
		return cached, nil
	}

	duration := time.Duration(days) * 24 * time.Hour
	fractions, err := s.listTimeSeriesPoints(ctx, "custom.googleapis.com/quota/remaining_fraction", duration)
	if err != nil {
		return model.QuotaHistoryResponse{}, fmt.Errorf("failed to retrieve historical remaining fraction: %w", err)
	}

	response := model.QuotaHistoryResponse{History: fractions}
	s.setCachedHistory(days, response)

	return response, nil
}

func (s *quotaService) listTimeSeriesPoints(ctx context.Context, metricType string, duration time.Duration) (map[string][]model.HistoricalPoint, error) {
	now := time.Now()
	startTime := now.Add(-duration)

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

	results := make(map[string][]model.HistoricalPoint)
	for _, ts := range timeSeriesList {
		quotaName := ts.Metric.Labels["quota_name"]
		if quotaName == "" {
			continue
		}
		if len(ts.Points) == 0 {
			continue
		}

		var points []model.HistoricalPoint
		for _, p := range ts.Points {
			var val float64
			switch p.Value.Value.(type) {
			case *monitoringpb.TypedValue_DoubleValue:
				val = p.Value.GetDoubleValue()
			case *monitoringpb.TypedValue_Int64Value:
				val = float64(p.Value.GetInt64Value())
			default:
				continue
			}
			points = append(points, model.HistoricalPoint{
				Timestamp: p.Interval.EndTime.AsTime().UTC(),
				Value:     val,
			})
		}

		// Sort points chronologically (oldest first)
		sort.Slice(points, func(i, j int) bool {
			return points[i].Timestamp.Before(points[j].Timestamp)
		})

		results[quotaName] = points
	}
	return results, nil
}

// seedFakeData populates local in-memory store with mock quotas and histories for demo/dev purposes.
func (s *quotaService) seedFakeData() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()

	// 1. Set current states
	s.lastQuota = model.QuotaResponse{
		Quota: map[string]model.QuotaDetails{
			"gemini-pro-5h": {
				RemainingFraction: 0.85,
				ResetTime:         now.Add(4 * time.Hour),
				ResetInSeconds:    14400,
			},
			"gemini-flash-5h": {
				RemainingFraction: 0.42,
				ResetTime:         now.Add(2 * time.Hour),
				ResetInSeconds:    7200,
			},
			"3p-5h": {
				RemainingFraction: 0.15,
				ResetTime:         now.Add(1 * time.Hour),
				ResetInSeconds:    3600,
			},
		},
	}
	s.hasRecords = true

	// 2. Generate 24 hours of simulated historical data (25 points, hourly intervals)
	for i := 24; i >= 0; i-- {
		t := now.Add(-time.Duration(i) * time.Hour)

		// Gemini Pro: Wave oscillation dipping around midday and recovering
		proVal := 0.72 + 0.18*math.Sin(float64(24-i)*0.45)

		// Gemini Flash: Step decay dropping gradually and resetting every 8 hours
		flashHourIndex := float64((24 - i) % 8)
		flashVal := 0.90 - flashHourIndex*0.08

		// 3p-5h: Linear decline down to critical levels
		threePVal := 0.80 - float64(24-i)*0.028

		s.history = append(s.history, historicalRecord{
			Timestamp: t,
			Quota: model.QuotaResponse{
				Quota: map[string]model.QuotaDetails{
					"gemini-pro-5h": {
						RemainingFraction: math.Max(0.0, math.Min(1.0, proVal)),
						ResetTime:         t.Add(4 * time.Hour),
						ResetInSeconds:    14400,
					},
					"gemini-flash-5h": {
						RemainingFraction: math.Max(0.0, math.Min(1.0, flashVal)),
						ResetTime:         t.Add(2 * time.Hour),
						ResetInSeconds:    7200,
					},
					"3p-5h": {
						RemainingFraction: math.Max(0.0, math.Min(1.0, threePVal)),
						ResetTime:         t.Add(1 * time.Hour),
						ResetInSeconds:    3600,
					},
				},
			},
		})
	}
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
