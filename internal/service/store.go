package service

import (
	"agenticquota/internal/model"
	"context"
)

// QuotaStore defines the interface for storing and retrieving quota data.
type QuotaStore interface {
	GetQuota(ctx context.Context) (model.QuotaResponse, error)
	SaveQuota(ctx context.Context, quota model.QuotaResponse) error
	GetQuotaHistory(ctx context.Context, days int) (model.QuotaHistoryResponse, error)
}
