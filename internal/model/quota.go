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
