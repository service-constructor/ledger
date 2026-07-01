// Package config loads runtime configuration from the environment.
package config

import (
	"fmt"
	"os"
	"time"
)

// Config holds all runtime settings.
type Config struct {
	// DatabaseURL is the Postgres DSN.
	DatabaseURL string
	// GRPCAddr is the listen address for the gRPC server.
	GRPCAddr string
	// PlatformWallet receives captured fees.
	PlatformWallet string
	// TONAddress is the shared on-chain deposit address stamped on every account
	// (the demo distinguishes users by memo tag, not by address).
	TONAddress string
	// ShutdownTimeout bounds graceful shutdown.
	ShutdownTimeout time.Duration
}

// Load reads configuration from the environment, applying defaults.
func Load() (Config, error) {
	c := Config{
		DatabaseURL:     env("DATABASE_URL", "postgres://sc:sc@localhost:5432/ledger?sslmode=disable"),
		GRPCAddr:        env("GRPC_ADDR", ":9100"),
		PlatformWallet:  env("PLATFORM_WALLET", "wlt_platform"),
		TONAddress:      env("TON_ADDRESS", "UQDX6G9_n_aIDhLBabVxpQLWL7APzTpejF0bZ0oT0-3O8qeC"),
		ShutdownTimeout: 10 * time.Second,
	}
	if c.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}
	return c, nil
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
