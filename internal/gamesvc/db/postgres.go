package db

import (
	"context"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

var DB *pgxpool.Pool

// Connect initializes the connection pool
func Connect() (*pgxpool.Pool, error) {
	dsn := os.Getenv("POSTGRES_URL")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, err
	}

	// Try pinging to make sure it's valid
	if err := pool.Ping(ctx); err != nil {
		return nil, err
	}

	DB = pool

	return pool, nil
}

// ClosePool is for graceful shutdown
func ClosePool() {
	if DB != nil {
		DB.Close()
	}
}
