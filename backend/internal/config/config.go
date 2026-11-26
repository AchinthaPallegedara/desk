// Package config loads runtime configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

type Config struct {
	Env      string // "development" | "production"
	HTTPAddr string
	// PublicURL is the externally visible origin, e.g. https://desk.claviq.com.
	// It is the OAuth issuer and the base of preview links.
	PublicURL string

	// DatabaseURL connects as the non-owner app role (RLS applies).
	DatabaseURL string
	// MigrateDatabaseURL connects as the schema owner; used by `migrate` and `create-tenant`.
	MigrateDatabaseURL string

	// AppSecret (>= 32 chars) is the root key for session cookies, signed links and OAuth HMAC.
	AppSecret string

	DataDir  string // local data: font cache, local storage
	TypstBin string

	Storage StorageConfig

	ShutdownTimeout time.Duration
}

type StorageConfig struct {
	Driver string // "local" | "r2"

	R2AccountID       string
	R2AccessKeyID     string
	R2SecretAccessKey string
	R2Bucket          string
}

func Load() (Config, error) {
	c := Config{
		Env:                getenv("APP_ENV", "development"),
		HTTPAddr:           getenv("HTTP_ADDR", ":8080"),
		PublicURL:          strings.TrimRight(getenv("PUBLIC_URL", "http://localhost:3000"), "/"),
		DatabaseURL:        os.Getenv("DATABASE_URL"),
		MigrateDatabaseURL: os.Getenv("MIGRATE_DATABASE_URL"),
		AppSecret:          os.Getenv("APP_SECRET"),
		DataDir:            getenv("DATA_DIR", "./data"),
		TypstBin:           getenv("TYPST_BIN", "typst"),
		Storage: StorageConfig{
			Driver:            getenv("STORAGE_DRIVER", "local"),
			R2AccountID:       os.Getenv("R2_ACCOUNT_ID"),
			R2AccessKeyID:     os.Getenv("R2_ACCESS_KEY_ID"),
			R2SecretAccessKey: os.Getenv("R2_SECRET_ACCESS_KEY"),
			R2Bucket:          os.Getenv("R2_BUCKET"),
		},
		ShutdownTimeout: 15 * time.Second,
	}
	if c.DatabaseURL == "" {
		return c, fmt.Errorf("DATABASE_URL is required")
	}
	if len(c.AppSecret) < 32 {
		return c, fmt.Errorf("APP_SECRET must be at least 32 characters")
	}
	if c.IsProduction() && !strings.HasPrefix(c.PublicURL, "https://") {
		return c, fmt.Errorf("PUBLIC_URL must be https in production")
	}
	if c.Storage.Driver == "r2" && (c.Storage.R2AccountID == "" || c.Storage.R2Bucket == "" ||
		c.Storage.R2AccessKeyID == "" || c.Storage.R2SecretAccessKey == "") {
		return c, fmt.Errorf("STORAGE_DRIVER=r2 requires R2_ACCOUNT_ID, R2_BUCKET, R2_ACCESS_KEY_ID, R2_SECRET_ACCESS_KEY")
	}
	return c, nil
}

func (c Config) IsProduction() bool { return c.Env == "production" }

// SecureCookies is true when the public URL is https.
func (c Config) SecureCookies() bool { return strings.HasPrefix(c.PublicURL, "https://") }

func getenv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}
