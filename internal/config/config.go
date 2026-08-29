// Package config loads and validates envy.yaml project configuration.
package config

import (
	"fmt"
	"os"
	"sort"

	"gopkg.in/yaml.v3"
)

// SchemaField describes one declared environment variable.
type SchemaField struct {
	Required bool     `yaml:"required"`
	Secret   bool     `yaml:"secret"`
	Type     string   `yaml:"type"`
	Default  string   `yaml:"default"`
	Values   []string `yaml:"values"`
	Min      *int     `yaml:"min"`
	Max      *int     `yaml:"max"`
}

// Environment declares where an environment's values live.
type Environment struct {
	File     string `yaml:"file"`
	Provider string `yaml:"provider"`
	Path     string `yaml:"path"`
	Protected bool  `yaml:"protected"`
}

// AgentPerms are per-environment agent permissions.
type AgentPerms struct {
	Metadata   bool `yaml:"metadata"`
	ReadValues bool `yaml:"read_values"`
	Write      bool `yaml:"write"`
	Delete     bool `yaml:"delete"`
}

// Config is the root envy.yaml document.
type Config struct {
	Version      int                           `yaml:"version"`
	ProjectName  string                        `yaml:"project"`
	Environments map[string]Environment        `yaml:"environments"`
	Schema       map[string]SchemaField        `yaml:"schema"`
	Agents       map[string]map[string]AgentPerms `yaml:"agents"`
}

// Load reads envy.yaml from path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if cfg.Environments == nil {
		cfg.Environments = map[string]Environment{}
	}
	if cfg.Schema == nil {
		cfg.Schema = map[string]SchemaField{}
	}
	if cfg.Agents == nil {
		cfg.Agents = map[string]map[string]AgentPerms{}
	}
	if cfg.Version == 0 {
		cfg.Version = 1
	}
	return &cfg, nil
}

// EnvironmentNames returns sorted environment names.
func (c *Config) EnvironmentNames() []string {
	names := make([]string, 0, len(c.Environments))
	for n := range c.Environments {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// IsSecret reports whether a key is marked secret in schema (or type secret).
func (c *Config) IsSecret(key string) bool {
	f, ok := c.Schema[key]
	if !ok {
		return false
	}
	return f.Secret || f.Type == "secret"
}

// IsRequired reports whether a key is required.
func (c *Config) IsRequired(key string) bool {
	f, ok := c.Schema[key]
	return ok && f.Required
}

// IsProtected reports whether an environment requires elevated trust to edit.
// An environment is protected when envy.yaml sets protected: true.
func (c *Config) IsProtected(env string) bool {
	e, ok := c.Environments[env]
	if !ok {
		return false
	}
	return e.Protected
}

// SourceLabel returns human-readable provider/file metadata for an environment.
func (c *Config) SourceLabel(env string) string {
	e, ok := c.Environments[env]
	if !ok {
		return "unknown"
	}
	if e.Provider != "" {
		path := e.Path
		if path == "" {
			path = "—"
		}
		return fmt.Sprintf("provider:%s path:%s", e.Provider, path)
	}
	if e.File != "" {
		return fmt.Sprintf("file:%s", e.File)
	}
	return "unconfigured"
}
