package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"agenticquota/internal/model"

	monitoring "cloud.google.com/go/monitoring/apiv3/v2"
	"cloud.google.com/go/monitoring/apiv3/v2/monitoringpb"
	"google.golang.org/api/iterator"
	"google.golang.org/genproto/googleapis/api/metric"
	"google.golang.org/genproto/googleapis/api/monitoredres"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

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

type gcpStore struct {
	projectID string
	client    gcpClient
}

func (s *gcpStore) GetQuota(ctx context.Context) (model.QuotaResponse, error) {
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

func (s *gcpStore) SaveQuota(ctx context.Context, quota model.QuotaResponse) error {
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

func (s *gcpStore) GetQuotaHistory(ctx context.Context, days int) (model.QuotaHistoryResponse, error) {
	duration := time.Duration(days) * 24 * time.Hour
	fractions, err := s.listTimeSeriesPoints(ctx, "custom.googleapis.com/quota/remaining_fraction", duration)
	if err != nil {
		return model.QuotaHistoryResponse{}, fmt.Errorf("failed to retrieve historical remaining fraction: %w", err)
	}

	return model.QuotaHistoryResponse{History: fractions}, nil
}

func (s *gcpStore) GetQuotaResetHistory(ctx context.Context, days int) (model.QuotaResetHistoryResponse, error) {
	duration := time.Duration(days) * 24 * time.Hour
	pointsMap, err := s.listTimeSeriesPoints(ctx, "custom.googleapis.com/quota/reset_time_epoch", duration)
	if err != nil {
		return model.QuotaResetHistoryResponse{}, fmt.Errorf("failed to retrieve historical reset time epoch: %w", err)
	}

	historyMap := make(map[string][]model.HistoricalResetPoint)
	for name, points := range pointsMap {
		var resetPoints []model.HistoricalResetPoint
		seen := make(map[int64]bool)
		for _, p := range points {
			unixSec := int64(p.Value)
			if unixSec <= 0 {
				continue
			}
			if !seen[unixSec] {
				seen[unixSec] = true
				resetPoints = append(resetPoints, model.HistoricalResetPoint{
					ResetTime: time.Unix(unixSec, 0).UTC(),
				})
			}
		}
		historyMap[name] = resetPoints
	}

	return model.QuotaResetHistoryResponse{History: historyMap}, nil
}

func (s *gcpStore) listMetric(ctx context.Context, metricType string) (map[string]interface{}, error) {
	now := time.Now()
	// Fetch the latest quota data within the last 7 days.
	startTime := now.Add(-7 * 24 * time.Hour)

	req := &monitoringpb.ListTimeSeriesRequest{
		Name:   "projects/" + s.projectID,
		Filter: fmt.Sprintf(`metric.type = "%s"`, metricType),
		Interval: &monitoringpb.TimeInterval{
			StartTime: timestamppb.New(startTime),
			EndTime:   timestamppb.New(now),
		},
		Aggregation: &monitoringpb.Aggregation{
			AlignmentPeriod:  durationpb.New(7 * 24 * time.Hour),
			PerSeriesAligner: monitoringpb.Aggregation_ALIGN_NEXT_OLDER,
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

func (s *gcpStore) listTimeSeriesPoints(ctx context.Context, metricType string, duration time.Duration) (map[string][]model.HistoricalPoint, error) {
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
