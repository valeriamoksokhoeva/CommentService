package config

import (
	"fmt"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	Storage     string
	Port        int
	DatabaseURL string
}

func Load() (*Config, error) {
	_ = godotenv.Load()

	storage := getEnv("STORAGE", "memory")

	port, err := strconv.Atoi(getEnv("PORT", "8080"))
	if err != nil || port < 1 || port > 65535 {
		return nil, fmt.Errorf("invalid PORT")
	}

	cfg := &Config{
		Storage: storage,
		Port:    port,
	}

	switch storage {
	case "memory":
		return cfg, nil

	case "postgres":
		cfg.DatabaseURL = os.Getenv("DATABASE_URL")
		if cfg.DatabaseURL == "" {
			return nil, fmt.Errorf(
				"DATABASE_URL is required when STORAGE=postgres",
			)
		}

		return cfg, nil

	default:
		return nil, fmt.Errorf("unknown STORAGE value: %s", storage)
	}
}

func getEnv(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}
