package service

import (
	"context"
	"math"
	"sync"
	"time"

	"agenticquota/internal/model"
)

type historicalRecord struct {
	Timestamp time.Time
	Quota     model.QuotaResponse
}

type inMemoryStore struct {
	mu         sync.RWMutex
	lastQuota  model.QuotaResponse
	hasRecords bool
	history    []historicalRecord
}

func newInMemoryStore(seed bool) *inMemoryStore {
	s := &inMemoryStore{}
	if seed {
		s.seedFakeData()
	}
	return s
}

func (s *inMemoryStore) GetQuota(ctx context.Context) (model.QuotaResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.hasRecords {
		return model.QuotaResponse{}, ErrNotFound
	}
	return s.lastQuota, nil
}

func (s *inMemoryStore) SaveQuota(ctx context.Context, quota model.QuotaResponse) error {
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

func (s *inMemoryStore) GetQuotaHistory(ctx context.Context, days int) (model.QuotaHistoryResponse, error) {
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

func (s *inMemoryStore) GetQuotaResetHistory(ctx context.Context, days int) (model.QuotaResetHistoryResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cutoff := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
	historyMap := make(map[string][]model.HistoricalResetPoint)
	seenMap := make(map[string]map[int64]bool)

	for _, record := range s.history {
		if record.Timestamp.Before(cutoff) {
			continue
		}
		for name, details := range record.Quota.Quota {
			if details.ResetTime.IsZero() {
				continue
			}
			resetUTC := details.ResetTime.UTC()
			unixSec := resetUTC.Unix()

			if _, ok := seenMap[name]; !ok {
				seenMap[name] = make(map[int64]bool)
			}
			if !seenMap[name][unixSec] {
				seenMap[name][unixSec] = true
				historyMap[name] = append(historyMap[name], model.HistoricalResetPoint{
					ResetTime: resetUTC,
				})
			}
		}
	}
	return model.QuotaResetHistoryResponse{History: historyMap}, nil
}

// seedFakeData populates local in-memory store with mock quotas and histories for demo/dev purposes.
func (s *inMemoryStore) seedFakeData() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()

	proResetNow := getResetTime(now, 5)
	flashResetNow := getResetTime(now, 8)
	threePResetNow := getResetTime(now, 5)

	// 1. Set current states
	s.lastQuota = model.QuotaResponse{
		Quota: map[string]model.QuotaDetails{
			"gemini-pro-5h": {
				RemainingFraction: 0.85,
				ResetTime:         proResetNow,
				ResetInSeconds:    int64(proResetNow.Sub(now).Seconds()),
			},
			"gemini-flash-5h": {
				RemainingFraction: 0.42,
				ResetTime:         flashResetNow,
				ResetInSeconds:    int64(flashResetNow.Sub(now).Seconds()),
			},
			"3p-5h": {
				RemainingFraction: 0.15,
				ResetTime:         threePResetNow,
				ResetInSeconds:    int64(threePResetNow.Sub(now).Seconds()),
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

		proReset := getResetTime(t, 5)
		flashReset := getResetTime(t, 8)
		threePReset := getResetTime(t, 5)

		s.history = append(s.history, historicalRecord{
			Timestamp: t,
			Quota: model.QuotaResponse{
				Quota: map[string]model.QuotaDetails{
					"gemini-pro-5h": {
						RemainingFraction: math.Max(0.0, math.Min(1.0, proVal)),
						ResetTime:         proReset,
						ResetInSeconds:    int64(proReset.Sub(t).Seconds()),
					},
					"gemini-flash-5h": {
						RemainingFraction: math.Max(0.0, math.Min(1.0, flashVal)),
						ResetTime:         flashReset,
						ResetInSeconds:    int64(flashReset.Sub(t).Seconds()),
					},
					"3p-5h": {
						RemainingFraction: math.Max(0.0, math.Min(1.0, threePVal)),
						ResetTime:         threePReset,
						ResetInSeconds:    int64(threePReset.Sub(t).Seconds()),
					},
				},
			},
		})
	}
}

// getResetTime calculates a stable reset time based on the timestamp and a period in hours.
func getResetTime(t time.Time, periodHours int) time.Time {
	unixHours := t.Unix() / 3600
	blockIndex := unixHours / int64(periodHours)
	return time.Unix((blockIndex+1)*int64(periodHours)*3600, 0).UTC()
}
