package service

import (
	"context"

	"github.com/avvvet/bingo-services/internal/gamesvc/store"
	"github.com/shopspring/decimal"
)

type BalanceService struct {
	balanceStore *store.BalanceStore
}

func NewBalanceService(store *store.BalanceStore) *BalanceService {
	return &BalanceService{balanceStore: store}
}

func (s *BalanceService) GetUserBalance(ctx context.Context, userID int64) (decimal.Decimal, error) {
	return s.balanceStore.GetBalanceByUserID(ctx, userID)
}
