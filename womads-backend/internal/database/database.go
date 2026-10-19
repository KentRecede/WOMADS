// Package database opens the GORM connection to PostgreSQL (Supabase).
package database

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Connect opens a pooled connection and verifies it with a ping.
//
// PreferSimpleProtocol is on because Supabase's "transaction" pooler
// (port 6543) does not support prepared statements; this setting makes the
// code work with the session pooler, transaction pooler and direct connection.
func Connect(databaseURL string) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.New(postgres.Config{
		DSN:                  databaseURL,
		PreferSimpleProtocol: true,
	}), &gorm.Config{
		// Warn level + ParameterizedQueries keeps applicant data out of the logs.
		Logger: logger.New(slogWriter{}, logger.Config{
			SlowThreshold:             300 * time.Millisecond,
			LogLevel:                  logger.Warn,
			IgnoreRecordNotFoundError: true,
			ParameterizedQueries:      true,
		}),
		TranslateError: true, // map driver errors to gorm.ErrDuplicatedKey etc.
	})
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(10)
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetConnMaxLifetime(30 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := sqlDB.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return db, nil
}

// slogWriter routes GORM's log lines into the structured logger.
type slogWriter struct{}

func (slogWriter) Printf(format string, args ...any) {
	slog.Warn(fmt.Sprintf(format, args...), "component", "gorm")
}
