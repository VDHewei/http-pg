package config

import (
	"encoding/json"
	"os"
)

type Config struct {
	ServerAddr      string `json:"server_addr"`
	ProxyAddr       string `json:"proxy_addr"`
	PostgresDSN     string `json:"postgres_dsn"`
	EncryptionKey   string `json:"encryption_key"`
	MaxConnections  int32  `json:"max_connections"`
	MinConnections  int32  `json:"min_connections"`
}

func DefaultConfig() *Config {
	return &Config{
		ServerAddr:     ":8080",
		ProxyAddr:      ":5432",
		PostgresDSN:    "postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable",
		EncryptionKey:  "",
		MaxConnections: 20,
		MinConnections: 5,
	}
}

func LoadConfig(path string) (*Config, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			cfg.EncryptionKey = os.Getenv("ENCRYPTION_KEY")
			return cfg, nil
		}
		return nil, err
	}

	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	if cfg.EncryptionKey == "" {
		cfg.EncryptionKey = os.Getenv("ENCRYPTION_KEY")
	}

	return cfg, nil
}
