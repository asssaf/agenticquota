package model

import "time"

// QuotaDetails represents the statistics of a specific quota.
type QuotaDetails struct {
	RemainingFraction float64   `json:"remaining_fraction"`
	ResetTime         time.Time `json:"reset_time"`
	ResetInSeconds    int64     `json:"reset_in_seconds"`
}

// QuotaResponse represents the top-level API response.
type QuotaResponse struct {
	Quota map[string]QuotaDetails `json:"quota"`
}

// HistoricalPoint represents a single metric data point in time.
type HistoricalPoint struct {
	Timestamp time.Time `json:"timestamp"`
	Value     float64   `json:"value"`
}

// QuotaHistoryResponse represents the historical metrics API response.
type QuotaHistoryResponse struct {
	History map[string][]HistoricalPoint `json:"history"`
}

// HistoricalResetPoint represents a single historical reset time point in time.
type HistoricalResetPoint struct {
	ResetTime time.Time `json:"reset_time"`
}

// QuotaResetHistoryResponse represents the historical reset times API response.
type QuotaResetHistoryResponse struct {
	History map[string][]HistoricalResetPoint `json:"history"`
}
