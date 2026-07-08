package service

import (
	"errors"
	"sync"

	"agenticquota/internal/model"
)

// ErrNotFound is returned when no quota data is found in the store.
var ErrNotFound = errors.New("no quota data found")

// QuotaService provides operations on quotas.
type QuotaService interface {
	GetQuota() (model.QuotaResponse, error)
	SaveQuota(quota model.QuotaResponse) error
}

type quotaService struct {
	mu         sync.RWMutex
	lastQuota  model.QuotaResponse
	hasRecords bool
}

// NewQuotaService creates a new instance of QuotaService.
func NewQuotaService() QuotaService {
	return &quotaService{}
}

// GetQuota returns the last saved quota details.
func (s *quotaService) GetQuota() (model.QuotaResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.hasRecords {
		return model.QuotaResponse{}, ErrNotFound
	}
	return s.lastQuota, nil
}

// SaveQuota stores a new quota report.
func (s *quotaService) SaveQuota(quota model.QuotaResponse) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.lastQuota = quota
	s.hasRecords = true
	return nil
}
