package service

import (
	"context"
	"math"
	"sort"
	"sync"
	"time"

	"agenticquota/internal/model"
)

type inMemoryQuotaService struct {
	mu         sync.RWMutex
	lastQuota  model.QuotaResponse
	hasRecords bool
	history    []historicalRecord
}

func (s *inMemoryQuotaService) GetQuota(ctx context.Context) (model.QuotaResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.hasRecords {
		return model.QuotaResponse{}, ErrNotFound
	}
	return s.lastQuota, nil
}

func (s *inMemoryQuotaService) SaveQuota(ctx context.Context, quota model.QuotaResponse) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()

	// Fetch previous quota and timestamps from s.history
	quotaMap := make(map[string]model.QuotaDetails)
	timeMap := make(map[string]time.Time)
	for i := len(s.history) - 1; i >= 0; i-- {
		record := s.history[i]
		for name, details := range record.Quota.Quota {
			if _, ok := quotaMap[name]; !ok {
				quotaMap[name] = details
				timeMap[name] = record.Timestamp
			}
		}
	}

	// Check for resets and insert reset records
	for name := range quota.Quota {
		tPrev, okT := timeMap[name]
		prevDetails, okQ := quotaMap[name]
		if okT && okQ {
			rtPrev := prevDetails.ResetTime
			if !rtPrev.IsZero() && rtPrev.After(tPrev) && rtPrev.Before(now) {
				// Reset occurred! Insert a reset historicalRecord at rtPrev
				resetRecord := historicalRecord{
					Timestamp: rtPrev,
					Quota: model.QuotaResponse{
						Quota: map[string]model.QuotaDetails{
							name: {
								RemainingFraction: 1.0,
								ResetTime:         rtPrev,
								ResetInSeconds:    0,
							},
						},
					},
				}
				s.history = insertHistoricalRecordSorted(s.history, resetRecord)
			}
		}
	}

	s.lastQuota = quota
	s.hasRecords = true

	// Record history in-memory
	s.history = insertHistoricalRecordSorted(s.history, historicalRecord{
		Timestamp: now,
		Quota:     quota,
	})

	// Limit in-memory history size to prevent memory exhaustion (up to 7 days)
	const maxHistory = 3000
	if len(s.history) > maxHistory {
		s.history = s.history[len(s.history)-maxHistory:]
	}

	// Prune records older than 7 days
	cutoff := now.Add(-7 * 24 * time.Hour)
	idx := 0
	for idx < len(s.history) && s.history[idx].Timestamp.Before(cutoff) {
		idx++
	}
	if idx > 0 {
		s.history = s.history[idx:]
	}

	return nil
}

func (s *inMemoryQuotaService) GetQuotaHistory(ctx context.Context, days int) (model.QuotaHistoryResponse, error) {
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

// seedFakeData populates local in-memory store with mock quotas and histories for demo/dev purposes.
func (s *inMemoryQuotaService) seedFakeData() {
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

func insertHistoricalRecordSorted(history []historicalRecord, record historicalRecord) []historicalRecord {
	idx := sort.Search(len(history), func(i int) bool {
		return history[i].Timestamp.After(record.Timestamp)
	})
	history = append(history, historicalRecord{})
	copy(history[idx+1:], history[idx:])
	history[idx] = record
	return history
}
