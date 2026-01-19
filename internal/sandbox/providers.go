package sandbox

import (
	"fmt"
	"strings"
)

// DefaultProviderRegistry builds the provider registry from config.
func DefaultProviderRegistry(cfg Config) (*ProviderRegistry, map[string]error) {
	registry := NewProviderRegistry()
	errs := map[string]error{}

	if client, err := GetDaytonaClient(); err == nil {
		registry.Register(newDaytonaProvider(client))
	} else {
		errs[ProviderDaytona] = err
	}

	return registry, errs
}

// ResolveProvider returns the provider instance and resolved name.
func ResolveProvider(cfg Config, cwd string, override string) (Provider, string, error) {
	name := ResolveProviderName(cfg, override)
	if name == "" {
		name = ProviderDaytona
	}

	registry, errs := DefaultProviderRegistry(cfg)
	if registry == nil {
		return nil, name, fmt.Errorf("no providers registered")
	}
	provider, ok := registry.Get(name)
	if !ok {
		if err, ok := errs[name]; ok {
			return nil, name, err
		}
		available := registry.List()
		if len(available) == 0 {
			return nil, name, fmt.Errorf("provider %q is unavailable (no providers registered)", name)
		}
		return nil, name, fmt.Errorf("provider %q is unavailable. Available: %s", name, strings.Join(available, ", "))
	}
	return provider, name, nil
}
