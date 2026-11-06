// path: internal/store/db.go
package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ConnectDB establishes a pooled connection to PostgreSQL and verifies connectivity.
func ConnectDB(ctx context.Context, pgURL string) (*pgxpool.Pool, error) {
	if pgURL == "" {
		return nil, fmt.Errorf("postgres connection URL is empty")
	}

	config, err := pgxpool.ParseConfig(pgURL)
	if err != nil {
		return nil, fmt.Errorf("parsing postgres connection URL: %w", err)
	}

	config.MaxConns = 25
	config.MinConns = 2
	config.MaxConnLifetime = 1 * time.Hour
	config.MaxConnIdleTime = 30 * time.Minute

	connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	pool, err := pgxpool.NewWithConfig(connectCtx, config)
	if err != nil {
		return nil, fmt.Errorf("creating postgres connection pool: %w", err)
	}

	if err := pool.Ping(connectCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("pinging postgres database: %w", err)
	}

	return pool, nil
}

