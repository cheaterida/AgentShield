// Package config provides environment-driven configuration with sensible defaults.
package config

import (
	"os"
	"strconv"
)

type Config struct {
	HTTPAddr       string
	GRPCAddr       string
	DBDriver       string // "sqlite" or "postgres"
	SQLitePath     string
	PostgresDSN    string
	OPABaseURL     string
	MLPipelineURL  string
	AuditBufferCap int
	LogLevel       string
}

func Load() Config {
	return Config{
		HTTPAddr:       getenv("AGENTSHIELD_HTTP_ADDR", ":8080"),
		GRPCAddr:       getenv("AGENTSHIELD_GRPC_ADDR", ":9090"),
		DBDriver:       getenv("AGENTSHIELD_DB_DRIVER", "sqlite"),
		SQLitePath:     getenv("AGENTSHIELD_SQLITE_PATH", "./data/agentshield.db"),
		PostgresDSN:    getenv("AGENTSHIELD_POSTGRES_DSN", ""),
		OPABaseURL:     getenv("AGENTSHIELD_OPA_BASE_URL", "http://localhost:8181"),
		MLPipelineURL:  getenv("AGENTSHIELD_ML_PIPELINE_URL", ""),
		AuditBufferCap: getenvInt("AGENTSHIELD_AUDIT_BUFFER_CAP", 10000),
		LogLevel:       getenv("AGENTSHIELD_LOG_LEVEL", "info"),
	}
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
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
