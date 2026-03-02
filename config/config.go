package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type RuleConfig struct {
	MaxTokens  int64 `yaml:"max_tokens"`
	RefillRate int64 `yaml:"refill_rate"`
}

type LoggingConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

type ObservabilityConfig struct {
	MetricsPort string `yaml:"metrics_port"`
}

type Config struct {
	Server struct {
		Port string `yaml:"port"`
	} `yaml:"server"`

	Redis struct {
		Addr string `yaml:"addr"`
	} `yaml:"redis"`

	Logging       LoggingConfig       `yaml:"logging"`
	Observability ObservabilityConfig `yaml:"observability"`

	Defaults RuleConfig            `yaml:"defaults"`
	Actions  map[string]RuleConfig `yaml:"actions"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	if cfg.Server.Port == "" {
		return nil, fmt.Errorf("server.port is required")
	}
	if cfg.Defaults.MaxTokens <= 0 {
		return nil, fmt.Errorf("defaults.max_tokens must be > 0")
	}
	if cfg.Defaults.RefillRate <= 0 {
		return nil, fmt.Errorf("defaults.refill_rate must be > 0")
	}

	if cfg.Logging.Level == "" {
		cfg.Logging.Level = "info"
	}
	if cfg.Logging.Format == "" {
		cfg.Logging.Format = "text"
	}
	if cfg.Observability.MetricsPort == "" {
		cfg.Observability.MetricsPort = ":9091"
	}

	return &cfg, nil
}

func (c *Config) RuleFor(action string) RuleConfig {
	if rule, ok := c.Actions[action]; ok {
		return rule
	}
	return c.Defaults
}
