package cli

import (
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/tmux"
)

// Services is a lightweight service container for CLI commands.
// Unlike app.New(), it starts no goroutines, watchers, or UI.
type Services struct {
	Config   *config.Config
	Registry *data.Registry
	Store    *data.WorkspaceStore
	TmuxOpts tmux.Options
	Version  string
}

var cliTmuxTimeoutOverrideNanos atomic.Int64

// NewServices constructs the minimal service set needed by CLI commands.
func NewServices(version string) (*Services, error) {
	cfg, err := config.DefaultConfig()
	if err != nil {
		return nil, err
	}

	// Mirror the env-var setup from internal/app/app_init.go without importing app.
	setEnvIfNonEmpty("AMUX_TMUX_SERVER", cfg.UI.TmuxServer)
	setEnvIfNonEmpty("AMUX_TMUX_CONFIG", cfg.UI.TmuxConfigPath)

	registry := data.NewRegistry(cfg.Paths.RegistryPath)
	store := data.NewWorkspaceStore(cfg.Paths.MetadataRoot)
	store.SetDefaultAssistant(cfg.ResolvedDefaultAssistant())
	opts := tmux.DefaultOptions()
	if timeout := currentCLITmuxTimeoutOverride(); timeout > 0 {
		opts.CommandTimeout = timeout
	}

	return &Services{
		Config:   cfg,
		Registry: registry,
		Store:    store,
		TmuxOpts: opts,
		Version:  version,
	}, nil
}

func setEnvIfNonEmpty(key, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	_ = os.Setenv(key, value)
}

func setCLITmuxTimeoutOverride(timeout time.Duration) time.Duration {
	return time.Duration(cliTmuxTimeoutOverrideNanos.Swap(int64(timeout)))
}

func currentCLITmuxTimeoutOverride() time.Duration {
	return time.Duration(cliTmuxTimeoutOverrideNanos.Load())
}
