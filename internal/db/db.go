package db

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"log"
	"time"

	_ "github.com/lib/pq"
)

//go:embed schema_embed.sql
var schemaSQL string

// Connect opens a database/sql connection pool (via lib/pq) and
// applies the embedded schema. The schema file uses IF NOT EXISTS
// everywhere so it's safe to run on every boot — no external
// migration tool needed for a project this size.
func Connect(ctx context.Context, dsn string) (*sql.DB, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("could not open postgres connection: %w", err)
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)

	// Retry briefly: in docker-compose, the app container can start
	// slightly before postgres is ready to accept connections.
	for i := 0; i < 10; i++ {
		pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		err = db.PingContext(pingCtx)
		cancel()
		if err == nil {
			break
		}
		log.Printf("[db] waiting for postgres (attempt %d/10): %v", i+1, err)
		time.Sleep(2 * time.Second)
	}
	if err != nil {
		return nil, fmt.Errorf("could not connect to postgres: %w", err)
	}

	if _, err := db.ExecContext(ctx, schemaSQL); err != nil {
		return nil, fmt.Errorf("failed applying schema: %w", err)
	}
	log.Println("[db] connected and schema applied")
	return db, nil
}
