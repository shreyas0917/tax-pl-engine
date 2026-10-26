// Package config loads the typed application configuration from environment variables and
// fails fast when a required value is missing.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config is the complete runtime configuration.
type Config struct {
	AppEnv         string
	HTTPAddr       string
	DatabaseURL    string
	DBAppRole      string // role assumed by user-facing connections (RLS applies); empty disables
	DBSystemRole   string // role for cross-user maintenance (BYPASSRLS); empty = login role
	RedisURL       string
	JWTSecret      string
	FrontendOrigin string

	BlobDriver   string
	BlobLocalDir string
	S3Endpoint   string
	S3AccessKey  string
	S3SecretKey  string
	S3BucketRaw  string
	S3BucketRep  string

	RawFileTTL     time.Duration
	AccessTokenTTL time.Duration
	RefreshTTL     time.Duration
	MaxUploadBytes int64
	WorkerCount    int
}

// IsDev reports whether we run in development mode (text logs, relaxed cookies).
func (c Config) IsDev() bool { return c.AppEnv != "prod" && c.AppEnv != "production" }

// Load reads the environment. It also reads a .env file from the working directory or its
// parent when present, so `go run ./cmd/api` works from backend/ without extra tooling.
func Load() (Config, error) {
	loadDotEnv()
	c := Config{
		AppEnv:         get("APP_ENV", "dev"),
		HTTPAddr:       get("HTTP_ADDR", ":8080"),
		DatabaseURL:    get("DATABASE_URL", ""),
		DBAppRole:      get("DB_APP_ROLE", "taxpl_app"),
		DBSystemRole:   get("DB_SYSTEM_ROLE", "taxpl_system"),
		RedisURL:       get("REDIS_URL", ""),
		JWTSecret:      get("JWT_SECRET", ""),
		FrontendOrigin: get("FRONTEND_ORIGIN", "http://localhost:5173"),
		BlobDriver:     get("BLOB_DRIVER", "local"),
		BlobLocalDir:   get("BLOB_LOCAL_DIR", "./.data/blobs"),
		S3Endpoint:     get("S3_ENDPOINT", ""),
		S3AccessKey:    get("S3_ACCESS_KEY", ""),
		S3SecretKey:    get("S3_SECRET_KEY", ""),
		S3BucketRaw:    get("S3_BUCKET_RAW", "taxpl-raw"),
		S3BucketRep:    get("S3_BUCKET_REPORTS", "taxpl-reports"),
		RawFileTTL:     time.Duration(getInt("RAW_FILE_TTL_HOURS", 24)) * time.Hour,
		AccessTokenTTL: time.Duration(getInt("ACCESS_TOKEN_MINUTES", 15)) * time.Minute,
		RefreshTTL:     time.Duration(getInt("REFRESH_TOKEN_DAYS", 30)) * 24 * time.Hour,
		MaxUploadBytes: int64(getInt("MAX_UPLOAD_MB", 20)) << 20,
		WorkerCount:    getInt("WORKER_COUNT", 4),
	}
	var missing []string
	if c.DatabaseURL == "" {
		missing = append(missing, "DATABASE_URL")
	}
	if c.JWTSecret == "" {
		missing = append(missing, "JWT_SECRET")
	}
	if len(missing) > 0 {
		return c, fmt.Errorf("missing required environment variables: %s", strings.Join(missing, ", "))
	}
	if !c.IsDev() && (len(c.JWTSecret) < 32 || strings.HasPrefix(c.JWTSecret, "change-me")) {
		return c, fmt.Errorf("JWT_SECRET must be a long random string in production")
	}
	if c.BlobDriver != "local" && c.BlobDriver != "s3" {
		return c, fmt.Errorf("BLOB_DRIVER must be local or s3, got %q", c.BlobDriver)
	}
	return c, nil
}

func get(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return def
}

func getInt(key string, def int) int {
	v := get(key, "")
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

// loadDotEnv sets variables from ./.env or ../.env without overriding existing ones.
func loadDotEnv() {
	for _, p := range []string{".env", "../.env"} {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(b), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			k, v, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			k, v = strings.TrimSpace(k), strings.Trim(strings.TrimSpace(v), `"'`)
			if _, exists := os.LookupEnv(k); !exists {
				_ = os.Setenv(k, v)
			}
		}
		return
	}
}
