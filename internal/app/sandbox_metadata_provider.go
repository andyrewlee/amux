package app

import (
	"os"
	"strings"

	"github.com/andyrewlee/amux/internal/sandbox"
)

func selectedSandboxMetadataProvider() string {
	cfg, err := loadSandboxConfig()
	if err != nil {
		return sandbox.ResolveProviderName(sandbox.Config{}, "")
	}
	return sandbox.ResolveProviderName(cfg, "")
}

func selectedSandboxSessionProviderFilter() string {
	value, ok := os.LookupEnv("AMUX_PROVIDER")
	if !ok {
		return ""
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return sandbox.ResolveProviderName(sandbox.Config{}, value)
}
