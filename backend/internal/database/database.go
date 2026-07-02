// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Package database provides GORM connection lifecycle helpers used by the
// AF backend. The package has no awareness of business logic; it only knows
// how to open a connection, ping it, and run migrations.
package database

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/skyzhao/af/internal/config"
	"github.com/skyzhao/af/internal/model"
)

// Open opens a GORM connection based on cfg.Driver.
//
// Supported drivers:
//   - "mysql"    — uses cfg.DSN()         (also the default when Driver is "")
//   - "postgres" — uses cfg.PostgresDSN() (aliases: "pgsql", "pg")
//   - "sqlite"   — uses cfg.Name as the file path or memory DSN
//     (e.g. ":memory:" or "file::memory:?cache=shared" or "/tmp/af.db")
func Open(cfg config.DBConfig) (*gorm.DB, error) {
	logLevel, err := parseGormLogLevel(cfg.LogLevel)
	if err != nil {
		return nil, err
	}

	gormCfg := &gorm.Config{
		Logger:                                   logger.Default.LogMode(logLevel),
		DisableForeignKeyConstraintWhenMigrating: false,
		PrepareStmt:                              true,
	}

	var (
		db  *gorm.DB
		openErr error
	)

	switch cfg.Driver {
	case "mysql", "":
		db, openErr = gorm.Open(mysql.Open(cfg.DSN()), gormCfg)
	case "postgres", "pgsql", "pg":
		db, openErr = gorm.Open(postgres.Open(cfg.PostgresDSN()), gormCfg)
	case "sqlite":
		// cfg.Name holds the SQLite DSN/path: e.g. ":memory:" or
		// "file::memory:?cache=shared" or "/tmp/af.db".
		dsn := cfg.Name
		if dsn == "" {
			dsn = ":memory:"
		}
		db, openErr = gorm.Open(sqlite.Open(dsn), gormCfg)
	default:
		return nil, fmt.Errorf("database: unsupported driver %q (want mysql|postgres|sqlite)", cfg.Driver)
	}
	if openErr != nil {
		return nil, fmt.Errorf("open db: %w", openErr)
	}

	// Connection-pool tuning. SQLite has no real connection pool, so skip
	// pool settings for it.
	if cfg.Driver != "sqlite" && cfg.MaxOpenConns > 0 {
		sqlDB, err := db.DB()
		if err != nil {
			return nil, fmt.Errorf("get sql.DB: %w", err)
		}
		sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
		if cfg.MaxIdleConns > 0 {
			sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
		}
		if cfg.ConnMaxLifetime > 0 {
			sqlDB.SetConnMaxLifetime(cfg.ConnMaxLifetime)
		}
	}

	return db, nil
}

// Migrate runs db.AutoMigrate against every model in the system. It is safe
// to call multiple times — GORM is idempotent.
func Migrate(db *gorm.DB) error {
	if db == nil {
		return errors.New("database: nil gorm.DB")
	}
	return model.Migrate(db)
}

// Ping verifies the connection is live.
func Ping(db *gorm.DB) error {
	if db == nil {
		return errors.New("database: nil gorm.DB")
	}
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("get sql.DB: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return sqlDB.PingContext(ctx)
}

// Close releases the underlying connection pool.
func Close(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

func parseGormLogLevel(s string) (logger.LogLevel, error) {
	switch s {
	case "", "info", "INFO":
		return logger.Info, nil
	case "warn", "WARN":
		return logger.Warn, nil
	case "error", "ERROR":
		return logger.Error, nil
	case "silent", "SILENT":
		return logger.Silent, nil
	default:
		return logger.Info, fmt.Errorf("database: invalid log level %q", s)
	}
}
