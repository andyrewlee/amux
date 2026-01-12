package computer

import (
	"errors"
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

	if token := ResolveSpritesToken(cfg); token != "" {
		registry.Register(newSpritesProvider(SpritesConfig{Token: token, APIURL: ResolveSpritesAPIURL(cfg)}))
	} else {
		errs[ProviderSprites] = errors.New("Sprites token not found. Set AMUX_SPRITES_TOKEN or SPRITES_TOKEN.")
	}

	registry.Register(newDockerProvider(DockerConfig{}))

	return registry, errs
}

// ResolveProvider returns the provider instance and resolved name.
func ResolveProvider(cfg Config, cwd string, override string) (Provider, string, error) {
	name := ResolveProviderName(cfg, override)
	if name == "" {
		return nil, "", errors.New("provider is required. Use --provider or AMUX_PROVIDER")
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
