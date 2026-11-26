// Package database opens the GORM connection and provides the tenant-scoped
// transaction helper that every tenant data access must go through.
package database

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"reflect"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Open connects as the app role. This role does not own the tables and has
// NOBYPASSRLS, so Postgres row-level security is always enforced.
func Open(dsn string, debug bool) (*gorm.DB, error) {
	level := logger.Warn
	if debug {
		level = logger.Info
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.New(log.New(os.Stdout, "", log.LstdFlags), logger.Config{
			SlowThreshold:             500 * time.Millisecond,
			LogLevel:                  level,
			IgnoreRecordNotFoundError: true,
			Colorful:                  false,
		}),
		NowFunc:                                  func() time.Time { return time.Now().UTC() },
		DisableForeignKeyConstraintWhenMigrating: true, // schema is owned by SQL migrations, never AutoMigrate
	})
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	if err := registerUUIDv7(db); err != nil {
		return nil, err
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	// Shared VPS: keep the pool small.
	sqlDB.SetMaxOpenConns(10)
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetConnMaxLifetime(30 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := sqlDB.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return db, nil
}

// registerUUIDv7 fills a zero uuid primary key "ID" with a UUIDv7 before insert.
func registerUUIDv7(db *gorm.DB) error {
	return db.Callback().Create().Before("gorm:create").Register("claviq:uuidv7", func(tx *gorm.DB) {
		if tx.Statement.Schema == nil {
			return
		}
		field := tx.Statement.Schema.LookUpField("ID")
		if field == nil || field.FieldType != reflect.TypeOf(uuid.UUID{}) {
			return
		}
		set := func(rv reflect.Value) {
			if v, zero := field.ValueOf(tx.Statement.Context, rv); zero || v.(uuid.UUID) == uuid.Nil {
				_ = field.Set(tx.Statement.Context, rv, uuid.Must(uuid.NewV7()))
			}
		}
		rv := tx.Statement.ReflectValue
		switch rv.Kind() {
		case reflect.Slice, reflect.Array:
			for i := 0; i < rv.Len(); i++ {
				set(reflect.Indirect(rv.Index(i)))
			}
		case reflect.Struct:
			set(rv)
		}
	})
}

var ErrNoTenant = errors.New("database: tenant id is required")

// WithTenant runs fn inside a transaction where app.tenant_id is set for RLS.
// set_config(..., true) is transaction-local, so the setting can never leak to
// another request through the connection pool.
//
// All reads and writes of tenant-owned tables MUST use the tx passed to fn.
func WithTenant(ctx context.Context, db *gorm.DB, tenantID uuid.UUID, fn func(tx *gorm.DB) error) error {
	if tenantID == uuid.Nil {
		return ErrNoTenant
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT set_config('app.tenant_id', ?, true)", tenantID.String()).Error; err != nil {
			return fmt.Errorf("set tenant: %w", err)
		}
		return fn(tx)
	})
}

// DBHandle wraps a connection opened for admin commands.
type DBHandle struct{ DB *gorm.DB }
