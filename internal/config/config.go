package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Default configuration values.
const (
	DefaultConfigFile      = "config.yaml"
	DefaultBrainPath       = "~/.kai/brain"
	DefaultSessionsPath    = "~/.kai/sessions"
	DefaultMaxContextFiles = 5
	DefaultListenAddr      = "0.0.0.0:8080"
	DefaultProviderTimeout = 120 // seconds
)

// ExpandPath expands a leading ~ to the user's home directory.
func ExpandPath(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, path[1:])
	}
	return path
}

// Default agent identity values.
const (
	DefaultAgentName = "kai"
	DefaultUserName  = "Kyle"
)

// Config is the top-level application configuration.
type Config struct {
	Agent        AgentConfig               `yaml:"agent"`
	Provider     string                    `yaml:"provider"`
	Providers    map[string]ProviderConfig `yaml:"providers"`
	Brain        BrainConfig               `yaml:"brain"`
	SessionsPath string                    `yaml:"sessions_path"`
	Server       ServerConfig              `yaml:"server"`
	Modules      []ModuleItemConfig        `yaml:"modules"`
}

// ModuleItemConfig holds the config for a single installable module.
type ModuleItemConfig struct {
	Name    string                 `yaml:"name"`
	Enabled bool                   `yaml:"enabled"`
	Config  map[string]interface{} `yaml:"config"`
}

// AgentConfig holds the agent and user identity.
type AgentConfig struct {
	Name     string `yaml:"name"`      // Display name for the AI agent (default: "kai")
	UserName string `yaml:"user_name"` // User's name for personalization (default: "User")
}

// ServerConfig holds HTTP server settings.
type ServerConfig struct {
	ListenAddr   string    `yaml:"listen_addr"`
	DashboardPIN string    `yaml:"dashboard_pin"` // PIN lock for dashboard (empty = no auth)
	SSO          SSOConfig `yaml:"sso"`
}

// SSOConfig holds OIDC single sign-on settings for the dashboard. Works with
// any OIDC-compliant provider (Dex, Okta, Azure AD, Google, ...) — Dex in
// particular speaks OIDC to client apps regardless of the upstream identity
// source (SAML, LDAP, GitHub, etc.) it federates to.
type SSOConfig struct {
	Issuer       string   `yaml:"issuer"` // OIDC issuer URL, e.g. https://dex.example.com
	ClientID     string   `yaml:"client_id"`
	ClientSecret string   `yaml:"client_secret"`
	RedirectURL  string   `yaml:"redirect_url"` // must match a redirect URI registered with the provider
	Scopes       []string `yaml:"scopes"`       // default: openid, profile, email
}

// Enabled reports whether SSO is configured.
func (c SSOConfig) Enabled() bool {
	return c.Issuer != "" && c.ClientID != ""
}

// ProviderConfig holds per-provider settings.
type ProviderConfig struct {
	Auth       string `yaml:"auth"`
	APIKey     string `yaml:"api_key"`
	Model      string `yaml:"model"`
	BaseURL    string `yaml:"base_url"`
	TimeoutSec int    `yaml:"timeout_sec"`
}

// BrainConfig holds brain storage settings.
type BrainConfig struct {
	Path            string      `yaml:"path"`
	MaxContextFiles int         `yaml:"max_context_files"`
	Sync            SyncConfig  `yaml:"sync"`
	Embeddings      EmbedConfig `yaml:"embeddings"`
	// VaultPin gates read/write of the reserved "vault/" category (supports
	// ${ENV}). Empty disables the category entirely — vault paths are then
	// rejected the same as any other unrecognized category.
	VaultPin string `yaml:"vault_pin"`
}

// EmbedConfig holds embedding settings for vector search.
type EmbedConfig struct {
	Provider string       `yaml:"provider"` // "ollama", "gemini" or "lsi" (default: auto — tries ollama, falls back to lsi)
	Ollama   OllamaConfig `yaml:"ollama"`
	Gemini   GeminiConfig `yaml:"gemini"`
}

// GeminiConfig holds Google Gemini embedding settings.
type GeminiConfig struct {
	APIKey  string `yaml:"api_key"`  // Google AI Studio API key (supports ${ENV})
	Model   string `yaml:"model"`    // Embedding model (default: text-embedding-004)
	BaseURL string `yaml:"base_url"` // Override API base (optional)
}

// OllamaConfig holds Ollama embedding settings.
type OllamaConfig struct {
	URL   string `yaml:"url"`   // Ollama API base URL (default: http://localhost:11434)
	Model string `yaml:"model"` // Embedding model name (default: nomic-embed-text)
}

// SyncConfig holds brain sync settings.
type SyncConfig struct {
	Git GitSyncConfig `yaml:"git"`
}

// GitSyncConfig holds git-based sync settings.
type GitSyncConfig struct {
	Enabled bool   `yaml:"enabled"`
	Remote  string `yaml:"remote"`
	Branch  string `yaml:"branch"`
}

var envVarPattern = regexp.MustCompile(`\$\{(\w+)\}`)

// Load reads and parses a config file, expanding ${ENV_VAR} references.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	// Expand ${ENV_VAR} references. Missing vars are replaced with empty
	// strings rather than failing — modules validate at runtime, so an
	// unset GITHUB_TOKEN shouldn't prevent the entire config from loading.
	expanded := envVarPattern.ReplaceAllFunc(data, func(match []byte) []byte {
		varName := string(envVarPattern.FindSubmatch(match)[1])
		return []byte(os.Getenv(varName))
	})

	cfg := Defaults()
	if err := yaml.Unmarshal(expanded, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	return cfg, nil
}

// Defaults returns a Config with sensible defaults and no provider.
func Defaults() *Config {
	return &Config{
		Agent: AgentConfig{
			Name:     DefaultAgentName,
			UserName: DefaultUserName,
		},
		Brain: BrainConfig{
			Path:            ExpandPath(DefaultBrainPath),
			MaxContextFiles: DefaultMaxContextFiles,
		},
		SessionsPath: ExpandPath(DefaultSessionsPath),
		Server: ServerConfig{
			ListenAddr: DefaultListenAddr,
		},
	}
}

// AgentName returns the configured agent name, falling back to default.
func (c *Config) AgentName() string {
	if c.Agent.Name != "" {
		return c.Agent.Name
	}
	return DefaultAgentName
}

// UserName returns the configured user name, falling back to default.
func (c *Config) UserName() string {
	if c.Agent.UserName != "" {
		return c.Agent.UserName
	}
	return DefaultUserName
}
