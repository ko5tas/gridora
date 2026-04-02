package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	MyEnergi   MyEnergiConfig    `yaml:"myenergi"`
	Export     ExportConfig      `yaml:"export"`
	Collection CollectionConfig  `yaml:"collection"`
	Server     ServerConfig      `yaml:"server"`
	Database   DatabaseConfig    `yaml:"database"`
	Logging    LoggingConfig     `yaml:"logging"`
	Milestones []MilestoneConfig `yaml:"milestones"`
}

// MilestoneConfig defines a vertical annotation line on the chart.
type MilestoneConfig struct {
	Date  string `yaml:"date" json:"date"`   // "2025-03-31"
	Label string `yaml:"label" json:"label"` // "Solar Panels Installed"
}

type MyEnergiConfig struct {
	HubSerial    string        `yaml:"hub_serial"`
	APIKey       string        `yaml:"api_key"`
	PollInterval time.Duration `yaml:"poll_interval"`
	RateLimit    time.Duration `yaml:"rate_limit"`
}

type ExportConfig struct {
	Enabled  bool     `yaml:"enabled"`
	Path     string   `yaml:"path"`
	Schedule string   `yaml:"schedule"`
	Time     string   `yaml:"time"`
	Formats  []string `yaml:"formats"`
	DBBackup bool     `yaml:"db_backup"`
}

type CollectionConfig struct {
	BackfillOnStartup  bool          `yaml:"backfill_on_startup"`
	BackfillRateLimit  time.Duration `yaml:"backfill_rate_limit"`
	DailyCollectionTime string       `yaml:"daily_collection_time"`
}

type ServerConfig struct {
	Listen string `yaml:"listen"`
}

type DatabaseConfig struct {
	Path string `yaml:"path"`
}

type LoggingConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

func Load(path string) (*Config, error) {
	cfg := defaults()

	// Environment variable overrides for secrets
	if v := os.Getenv("GRIDORA_HUB_SERIAL"); v != "" {
		cfg.MyEnergi.HubSerial = v
	}
	if v := os.Getenv("GRIDORA_API_KEY"); v != "" {
		cfg.MyEnergi.APIKey = v
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	// Env vars take precedence over file
	if v := os.Getenv("GRIDORA_HUB_SERIAL"); v != "" {
		cfg.MyEnergi.HubSerial = v
	}
	if v := os.Getenv("GRIDORA_API_KEY"); v != "" {
		cfg.MyEnergi.APIKey = v
	}

	// Expand ~ in paths
	cfg.Database.Path = expandHome(cfg.Database.Path)
	cfg.Export.Path = expandHome(cfg.Export.Path)

	if err := validate(cfg); err != nil {
		return nil, fmt.Errorf("config validation: %w", err)
	}

	return cfg, nil
}

// expandHome replaces a leading ~ with the user's home directory.
func expandHome(path string) string {
	if len(path) < 2 || path[0] != '~' || path[1] != '/' {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, path[2:])
}

func defaults() *Config {
	return &Config{
		MyEnergi: MyEnergiConfig{
			PollInterval: 60 * time.Second,
			RateLimit:    1 * time.Second,
		},
		Export: ExportConfig{
			Enabled:  true,
			Path:     "/var/lib/gridora/exports",
			Schedule: "daily",
			Time:     "01:00",
			Formats:  []string{"csv"},
			DBBackup: true,
		},
		Collection: CollectionConfig{
			BackfillOnStartup:   true,
			BackfillRateLimit:   5 * time.Second,
			DailyCollectionTime: "00:05",
		},
		Server: ServerConfig{
			Listen: ":8383",
		},
		Database: DatabaseConfig{
			Path: "/var/lib/gridora/gridora.db",
		},
		Logging: LoggingConfig{
			Level:  "info",
			Format: "text",
		},
	}
}

func validate(cfg *Config) error {
	if cfg.MyEnergi.HubSerial == "" {
		return fmt.Errorf("myenergi.hub_serial is required")
	}
	if cfg.MyEnergi.APIKey == "" {
		return fmt.Errorf("myenergi.api_key is required")
	}
	if cfg.MyEnergi.PollInterval < 10*time.Second {
		return fmt.Errorf("myenergi.poll_interval must be at least 10s")
	}
	if cfg.MyEnergi.RateLimit < 500*time.Millisecond {
		return fmt.Errorf("myenergi.rate_limit must be at least 500ms")
	}
	return nil
}
