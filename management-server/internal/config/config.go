// Package config provides environment-driven configuration with sensible defaults.
package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	HTTPAddr       string
	DBDriver       string // "sqlite" or "postgres"
	SQLitePath     string
	PostgresDSN    string
	RedisAddr      string
	RedisPassword  string
	RedisDB        int
	CacheTTL       time.Duration
	OPABaseURL     string
	MLPipelineURL  string
	AuditBufferCap       int
	LogLevel             string
	TokenQuotaEnabled    bool
	TokenQuotaDefaultDaily   int64
	TokenQuotaDefaultMonthly int64
	TokenQuotaAggInterval    int
	TokenQuotaLogRetention   int
}

func Load() Config {
	return Config{
		HTTPAddr:       getenv("AGENTSHIELD_HTTP_ADDR", ":8080"),
		DBDriver:       getenv("AGENTSHIELD_DB_DRIVER", "sqlite"),
		SQLitePath:     getenv("AGENTSHIELD_SQLITE_PATH", "./data/agentshield.db"),
		PostgresDSN:    getenv("AGENTSHIELD_POSTGRES_DSN", ""),
		RedisAddr:      getenv("AGENTSHIELD_REDIS_ADDR", "localhost:6379"),
		RedisPassword:  getenv("AGENTSHIELD_REDIS_PASSWORD", ""),
		RedisDB:        getenvInt("AGENTSHIELD_REDIS_DB", 0),
		CacheTTL:       getenvDur("AGENTSHIELD_CACHE_TTL", 5*time.Minute),
		OPABaseURL:     getenv("AGENTSHIELD_OPA_BASE_URL", "http://localhost:8181"),
		MLPipelineURL:  getenv("AGENTSHIELD_ML_PIPELINE_URL", ""),
		AuditBufferCap: getenvInt("AGENTSHIELD_AUDIT_BUFFER_CAP", 10000),
		LogLevel:       getenv("AGENTSHIELD_LOG_LEVEL", "info"),

		TokenQuotaEnabled:        getenvBool("AGENTSHIELD_TOKEN_QUOTA_ENABLED", true),
		TokenQuotaDefaultDaily:   int64(getenvInt("AGENTSHIELD_TOKEN_QUOTA_DEFAULT_DAILY", 1000000)),
		TokenQuotaDefaultMonthly: int64(getenvInt("AGENTSHIELD_TOKEN_QUOTA_DEFAULT_MONTHLY", 20000000)),
		TokenQuotaAggInterval:    getenvInt("AGENTSHIELD_TOKEN_QUOTA_AGGREGATION_INTERVAL", 300),
		TokenQuotaLogRetention:   getenvInt("AGENTSHIELD_TOKEN_QUOTA_LOG_RETENTION_DAYS", 90),
	}
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func getenvBool(k string, def bool) bool {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	return v == "true" || v == "1" || v == "yes"
}

func getenvInt(k string, def int) int {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func getenvDur(k string, def time.Duration) time.Duration {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return d
}
