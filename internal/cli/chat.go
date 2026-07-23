package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"sort"

	"github.com/norenis/kai/internal/auth"
	"github.com/norenis/kai/internal/brain"
	"github.com/norenis/kai/internal/config"
	"github.com/norenis/kai/internal/module"
	"github.com/norenis/kai/internal/prompt"
	"github.com/norenis/kai/internal/provider"
	"github.com/norenis/kai/internal/retriever"
	"github.com/norenis/kai/internal/router"
	"github.com/norenis/kai/internal/session"
)

// Credential storage paths relative to home directory.
const (
	credentialsDir  = ".kai"
	credentialsFile = "credentials.json"
)

func loadConfig() (*config.Config, error) {
	path := cfgFile
	if path == "" {
		// Search order:
		//   1. ./config.yaml  (local / dev override)
		//   2. ~/.kai/config.yaml  (installed default, created by `kai setup`)
		if _, err := os.Stat(config.DefaultConfigFile); err == nil {
			path = config.DefaultConfigFile
		} else if home, err := os.UserHomeDir(); err == nil {
			path = filepath.Join(home, "."+config.DefaultAgentName, "config.yaml")
		} else {
			path = config.DefaultConfigFile
		}
	}
	return config.Load(path)
}

// applyEmbedConfig configures the brain's embedding backend from config.
func applyEmbedConfig(b *brain.FileBrain, cfg *config.Config) {
	b.SetEmbedOptions(brain.EmbedOptions{
		Provider:      brain.EmbedProvider(cfg.Brain.Embeddings.Provider),
		OllamaURL:     cfg.Brain.Embeddings.Ollama.URL,
		OllamaModel:   cfg.Brain.Embeddings.Ollama.Model,
		GeminiAPIKey:  cfg.Brain.Embeddings.Gemini.APIKey,
		GeminiModel:   cfg.Brain.Embeddings.Gemini.Model,
		GeminiBaseURL: cfg.Brain.Embeddings.Gemini.BaseURL,
	})
}

func buildRouter(cfg *config.Config) (*router.Router, *brain.FileBrain, provider.Provider, *session.Manager, error) {
	// Brain
	b := brain.New(cfg.Brain.Path)
	applyEmbedConfig(b, cfg)

	// Ensure brain directories exist
	for _, dir := range brain.DefaultCategories {
		os.MkdirAll(filepath.Join(cfg.Brain.Path, dir), brain.DirPermissions)
	}

	// Initialize index if it doesn't exist
	if _, err := b.LoadIndex(); err != nil {
		if rebuildErr := b.RebuildIndex(); rebuildErr != nil {
			return nil, nil, nil, nil, fmt.Errorf("rebuild brain index: %w", rebuildErr)
		}
	}

	// Provider
	provCfg, ok := cfg.Providers[cfg.Provider]
	if !ok {
		return nil, nil, nil, nil, fmt.Errorf("provider %q not found in config", cfg.Provider)
	}

	apiKey := provCfg.APIKey
	if apiKey == "" {
		// Try keystore
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, nil, nil, nil, fmt.Errorf("cannot determine home directory: %w", err)
		}
		ks := auth.NewKeystore(filepath.Join(home, credentialsDir, credentialsFile))
		mgr := auth.NewManager(ks)
		key, err := mgr.GetCredential(cfg.Provider)
		if err != nil {
			return nil, nil, nil, nil, fmt.Errorf("no API key for %s. Run: kai auth set-key --provider %s", cfg.Provider, cfg.Provider)
		}
		apiKey = key
	}

	timeout := time.Duration(provCfg.TimeoutSec) * time.Second

	var prov provider.Provider
	switch cfg.Provider {
	case "claude":
		prov = provider.NewClaude(apiKey, provCfg.Model, provCfg.BaseURL, timeout)
	case "openai":
		prov = provider.NewOpenAI(apiKey, provCfg.Model, provCfg.BaseURL, timeout)
	case "gemini":
		prov = provider.NewGemini(apiKey, provCfg.Model, provCfg.BaseURL, timeout)
	default:
		return nil, nil, nil, nil, fmt.Errorf("unsupported provider: %s", cfg.Provider)
	}

	// Sessions
	sessPath := config.ExpandPath(cfg.SessionsPath)
	if sessPath == "" {
		sessPath = config.ExpandPath(config.DefaultSessionsPath)
	}
	sessMgr := session.NewManager(sessPath)

	// Retriever
	ret := retriever.New(b, cfg.Brain.MaxContextFiles)

	// Router
	r := router.New(b, ret, prompt.NewBuilder(cfg.AgentName(), cfg.UserName()), prov, sessMgr)

	return r, b, prov, sessMgr, nil
}

func runInteractiveChat(r *router.Router, b *brain.FileBrain, sessMgr *session.Manager, cfg *config.Config) error {
	sess := sessMgr.New(cfg.Provider)
	reader := bufio.NewReader(os.Stdin)
	ctx := context.Background()

	fmt.Printf("%s — your digital twin\n", cfg.AgentName())
	fmt.Println("Type /quit to exit, /brain to see context, /confidence for last confidence level, /module for plugins")
	fmt.Println()

	var lastResult *router.ChatResult

	for {
		fmt.Print("you> ")
		input, err := reader.ReadString('\n')
		if err != nil {
			break
		}
		input = strings.TrimSpace(input)

		if input == "" {
			continue
		}

		// Handle slash commands
		switch {
		case input == "/quit":
			sessMgr.Save(sess)
			fmt.Println("Session saved. Goodbye!")
			return nil
		case input == "/brain":
			if lastResult != nil {
				fmt.Printf("Brain files used: %v\n\n", lastResult.BrainFilesUsed)
			} else {
				fmt.Println("No previous response yet.")
			}
			continue
		case input == "/confidence":
			if lastResult != nil {
				fmt.Printf("Confidence: %s\n\n", lastResult.Confidence)
			} else {
				fmt.Println("No previous response yet.")
			}
			continue
		case input == "/teach":
			fmt.Println("Switching to teaching mode. Type /done to return to chat.")
			if err := runTeachMode(cfg); err != nil {
				fmt.Printf("Teaching mode error: %v\n\n", err)
			} else {
				fmt.Println("Back to chat mode.")
			}
			continue
		case strings.HasPrefix(input, "/forget "):
			topic := strings.TrimPrefix(input, "/forget ")
			fmt.Printf("Forgetting %q... (not yet implemented)\n\n", topic)
			continue
		case input == "/module" || input == "/module list":
			handleSlashModuleList(cfg)
			continue
		case input == "/module --all":
			handleSlashModuleRun(cfg, b, "")
			continue
		case strings.HasPrefix(input, "/module "):
			name := strings.TrimPrefix(input, "/module ")
			handleSlashModuleRun(cfg, b, name)
			continue
		}

		result, err := r.Chat(ctx, sess, input)
		if err != nil {
			fmt.Printf("Error: %v\n\n", err)
			continue
		}

		lastResult = result
		fmt.Printf("\n%s> %s\n\n", cfg.AgentName(), result.Response)
	}

	sessMgr.Save(sess)
	return nil
}

// handleSlashModuleList prints all registered modules and their enabled status.
func handleSlashModuleList(cfg *config.Config) {
	reg := module.Default()
	available := reg.Available()
	sort.Strings(available)

	enabled := make(map[string]bool, len(cfg.Modules))
	for _, mc := range cfg.Modules {
		if mc.Enabled {
			enabled[mc.Name] = true
		}
	}

	fmt.Println()
	for _, name := range available {
		tag := ""
		if enabled[name] {
			tag = " [enabled]"
		}
		fmt.Printf("%-22s%-10s %s\n", name, tag, reg.Description(name))
	}
	fmt.Println()
}

// handleSlashModuleRun runs a named module (or all enabled modules when name is empty).
func handleSlashModuleRun(cfg *config.Config, b *brain.FileBrain, name string) {
	if name == "" {
		if err := runEnabledModules(cfg, b); err != nil {
			fmt.Printf("module error: %v\n\n", err)
		}
		return
	}
	if err := execModule(cfg, b, name); err != nil {
		fmt.Printf("module error: %v\n\n", err)
	}
}

func runOneShotAsk(r *router.Router, sessMgr *session.Manager, cfg *config.Config, question string) error {
	sess := sessMgr.New(cfg.Provider)
	ctx := context.Background()

	result, err := r.Chat(ctx, sess, question)
	if err != nil {
		return fmt.Errorf("chat error: %w", err)
	}

	fmt.Println(result.Response)
	sessMgr.Save(sess)
	return nil
}
