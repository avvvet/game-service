package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
)

type BalanceStore struct {
	db *pgxpool.Pool
}

func NewBalanceStore(db *pgxpool.Pool) *BalanceStore {
	return &BalanceStore{db: db}
}

func (c *BalanceStore) GetBalanceByUserID(ctx context.Context, userId int64) (decimal.Decimal, error) {
	var totalDr, totalCr decimal.Decimal

	err := c.db.QueryRow(ctx, `
        SELECT 
            COALESCE(SUM(dr), 0), 
            COALESCE(SUM(cr), 0)
        FROM balances
        WHERE user_id = $1 AND status = 'completed'
    `, userId).Scan(&totalDr, &totalCr)

	if err != nil {
		return decimal.Zero, err
	}

	balance := totalDr.Sub(totalCr)
	return balance, nil
}
