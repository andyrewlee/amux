package service

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/andyrewlee/medusa/internal/config"
	"github.com/andyrewlee/medusa/internal/data"
	"github.com/andyrewlee/medusa/internal/logging"
)

// ConfigService manages settings, profiles, and permissions.
type ConfigService struct {
	config   *config.Config
	registry *data.Registry
	eventBus *EventBus
}

// NewConfigService creates a config service.
func NewConfigService(cfg *config.Config, registry *data.Registry, bus *EventBus) *ConfigService {
	return &ConfigService{
		config:   cfg,
		registry: registry,
		eventBus: bus,
	}
}

// GetConfig returns the current configuration.
func (s *ConfigService) GetConfig() *config.Config {
	return s.config
}

// GetSettings returns the current UI settings.
func (s *ConfigService) GetSettings() config.UISettings {
	return s.config.UI
}

// UpdateSettings updates the UI settings and persists them.
func (s *ConfigService) UpdateSettings(settings config.UISettings) error {
	s.config.UI = settings
	if err := s.config.SaveUISettings(); err != nil {
		return fmt.Errorf("saving settings: %w", err)
	}
	s.eventBus.Publish(NewEvent(EventSettingsChanged, settings))
	return nil
}

// ListProfiles returns all available profile names.
func (s *ConfigService) ListProfiles() ([]string, error) {
	profilesRoot := s.config.Paths.ProfilesRoot
	entries, err := os.ReadDir(profilesRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading profiles directory: %w", err)
	}

	var profiles []string
	for _, entry := range entries {
		if entry.IsDir() {
			profiles = append(profiles, entry.Name())
		}
	}
	return profiles, nil
}

// CreateProfile creates a new named profile directory.
func (s *ConfigService) CreateProfile(name string) error {
	if name == "" {
		return fmt.Errorf("profile name is required")
	}

	profileDir := filepath.Join(s.config.Paths.ProfilesRoot, name)
	if _, err := os.Stat(profileDir); err == nil {
		return fmt.Errorf("profile '%s' already exists", name)
	}

	if err := os.MkdirAll(profileDir, 0755); err != nil {
		return fmt.Errorf("creating profile directory: %w", err)
	}

	s.eventBus.Publish(NewEvent(EventProfileCreated, map[string]string{"name": name}))
	return nil
}

// RenameProfile renames a profile directory.
func (s *ConfigService) RenameProfile(oldName, newName string) error {
	if oldName == "" || newName == "" {
		return fmt.Errorf("both old and new profile names are required")
	}

	oldDir := filepath.Join(s.config.Paths.ProfilesRoot, oldName)
	newDir := filepath.Join(s.config.Paths.ProfilesRoot, newName)

	if _, err := os.Stat(oldDir); os.IsNotExist(err) {
		return fmt.Errorf("profile '%s' does not exist", oldName)
	}
	if _, err := os.Stat(newDir); err == nil {
		return fmt.Errorf("profile '%s' already exists", newName)
	}

	if err := os.Rename(oldDir, newDir); err != nil {
		return fmt.Errorf("renaming profile: %w", err)
	}

	// Update projects that reference this profile
	s.updateProjectProfiles(oldName, newName)

	return nil
}

// DeleteProfile removes a profile directory.
func (s *ConfigService) DeleteProfile(name string) error {
	if name == "" {
		return fmt.Errorf("profile name is required")
	}

	profileDir := filepath.Join(s.config.Paths.ProfilesRoot, name)
	if _, err := os.Stat(profileDir); os.IsNotExist(err) {
		return nil // Already gone
	}

	if err := os.RemoveAll(profileDir); err != nil {
		return fmt.Errorf("deleting profile: %w", err)
	}

	// Clear profile from projects that use it
	s.clearProjectProfile(name)

	s.eventBus.Publish(NewEvent(EventProfileDeleted, map[string]string{"name": name}))
	return nil
}

// GetGlobalPermissions returns the global permissions configuration.
func (s *ConfigService) GetGlobalPermissions() (*config.GlobalPermissions, error) {
	return config.LoadGlobalPermissions(s.config.Paths.GlobalPermissionsPath)
}

// UpdateGlobalPermissions saves the global permissions configuration.
func (s *ConfigService) UpdateGlobalPermissions(perms *config.GlobalPermissions) error {
	if err := config.SaveGlobalPermissions(s.config.Paths.GlobalPermissionsPath, perms); err != nil {
		return fmt.Errorf("saving global permissions: %w", err)
	}
	s.eventBus.Publish(NewEvent(EventPermissionsChanged, nil))
	return nil
}

// GetSandboxRules returns the sandbox rules configuration.
func (s *ConfigService) GetSandboxRules() (*config.SandboxRules, error) {
	rules, err := config.LoadSandboxRules(s.config.Paths.SandboxRulesPath)
	if err != nil {
		return config.DefaultSandboxRules(), nil
	}
	return rules, nil
}

// --- internal helpers ---

func (s *ConfigService) updateProjectProfiles(oldName, newName string) {
	if err := s.registry.RenameProfile(oldName, newName); err != nil {
		logging.Warn("Failed to rename project profiles: %v", err)
	}
	if err := s.registry.RenameGroupProfile(oldName, newName); err != nil {
		logging.Warn("Failed to rename group profiles: %v", err)
	}
}

func (s *ConfigService) clearProjectProfile(name string) {
	if err := s.registry.ClearProfile(name); err != nil {
		logging.Warn("Failed to clear project profile: %v", err)
	}
	if err := s.registry.ClearGroupProfile(name); err != nil {
		logging.Warn("Failed to clear group profile: %v", err)
	}
}
