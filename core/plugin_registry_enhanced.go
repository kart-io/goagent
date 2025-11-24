// Package core provides enhanced plugin registry with dependency injection.
//
// This file extends the basic PluginRegistry with:
//   - Version management (multiple versions of same plugin)
//   - Dependency injection container
//   - Configuration management
//   - Lifecycle integration
package core

import (
	"context"
	"fmt"
	"sync"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
)

// =============================================================================
// Dependency Injection Container
// =============================================================================

// Container provides dependency injection for plugins.
//
// Plugins can request shared resources like Logger, Store, LLM clients, etc.
// through the container instead of passing them as parameters.
type Container struct {
	mu        sync.RWMutex
	services  map[string]interface{}
	factories map[string]func() (interface{}, error)
}

// NewContainer creates a new dependency injection container.
func NewContainer() *Container {
	return &Container{
		services:  make(map[string]interface{}),
		factories: make(map[string]func() (interface{}, error)),
	}
}

// Register registers a service instance.
func (c *Container) Register(name string, instance interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.services[name] = instance
}

// RegisterFactory registers a factory function for lazy initialization.
func (c *Container) RegisterFactory(name string, factory func() (interface{}, error)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.factories[name] = factory
}

// Resolve retrieves a service by name.
// If the service is registered as a factory, it will be created on first access.
func (c *Container) Resolve(name string) (interface{}, error) {
	c.mu.RLock()
	// Check if already instantiated
	if service, exists := c.services[name]; exists {
		c.mu.RUnlock()
		return service, nil
	}

	// Check if factory exists
	factory, hasFactory := c.factories[name]
	c.mu.RUnlock()

	if !hasFactory {
		return nil, agentErrors.New(agentErrors.CodeNotFound, "service not found").
			WithComponent("container").
			WithOperation("resolve").
			WithContext("name", name)
	}

	// Create instance from factory
	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check after acquiring write lock
	if service, exists := c.services[name]; exists {
		return service, nil
	}

	instance, err := factory()
	if err != nil {
		return nil, agentErrors.Wrap(err, agentErrors.CodeInternal, "factory failed").
			WithComponent("container").
			WithOperation("resolve").
			WithContext("name", name)
	}

	c.services[name] = instance
	return instance, nil
}

// MustResolve resolves a service or panics.
func (c *Container) MustResolve(name string) interface{} {
	service, err := c.Resolve(name)
	if err != nil {
		panic(fmt.Sprintf("failed to resolve %s: %v", name, err))
	}
	return service
}

// ResolveTyped resolves a service with type assertion.
func ResolveTyped[T any](c *Container, name string) (T, error) {
	var zero T

	service, err := c.Resolve(name)
	if err != nil {
		return zero, err
	}

	typed, ok := service.(T)
	if !ok {
		return zero, agentErrors.New(agentErrors.CodeTypeMismatch, "service type mismatch").
			WithComponent("container").
			WithOperation("resolve_typed").
			WithContext("name", name)
	}

	return typed, nil
}

// =============================================================================
// Enhanced Plugin Interface
// =============================================================================

// Plugin represents a loadable plugin that can provide tools, agents, or other components.
//
// Plugins implement lifecycle management and can access shared resources through
// the dependency injection container.
type Plugin interface {
	interfaces.Lifecycle

	// Name returns the plugin's unique identifier.
	Name() string

	// Version returns the plugin's version string.
	Version() string

	// GetTools returns tools provided by this plugin.
	GetTools() []DynamicRunnable

	// GetAgents returns agents provided by this plugin.
	GetAgents() []DynamicRunnable

	// GetMiddleware returns middleware provided by this plugin.
	GetMiddleware() []interface{}
}

// PluginConfig contains configuration for a plugin instance.
type PluginConfig struct {
	// Name is the plugin identifier
	Name string

	// Version is the plugin version
	Version string

	// Config is plugin-specific configuration
	Config map[string]interface{}

	// Dependencies lists other plugins this plugin depends on
	Dependencies []PluginDependency

	// Enabled determines if the plugin should be loaded
	Enabled bool
}

// PluginDependency specifies a dependency on another plugin.
type PluginDependency struct {
	// Name of the required plugin
	Name string

	// VersionConstraint (e.g., ">=1.0.0", "~1.2.0")
	VersionConstraint string

	// Optional marks this dependency as optional
	Optional bool
}

// =============================================================================
// Enhanced Plugin Registry
// =============================================================================

// EnhancedPluginRegistry extends PluginRegistry with version management and DI.
type EnhancedPluginRegistry struct {
	mu        sync.RWMutex
	plugins   map[string]map[string]Plugin // name -> version -> plugin
	container *Container
	lifecycle *DefaultLifecycleManager
}

// NewEnhancedPluginRegistry creates a new enhanced plugin registry.
func NewEnhancedPluginRegistry() *EnhancedPluginRegistry {
	return &EnhancedPluginRegistry{
		plugins:   make(map[string]map[string]Plugin),
		container: NewContainer(),
		lifecycle: NewLifecycleManager(),
	}
}

// Container returns the dependency injection container.
func (r *EnhancedPluginRegistry) Container() *Container {
	return r.container
}

// Lifecycle returns the lifecycle manager.
func (r *EnhancedPluginRegistry) Lifecycle() *DefaultLifecycleManager {
	return r.lifecycle
}

// RegisterPlugin registers a plugin with optional version.
func (r *EnhancedPluginRegistry) RegisterPlugin(plugin Plugin, config *PluginConfig) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	name := plugin.Name()
	version := plugin.Version()

	// Create version map if needed
	if r.plugins[name] == nil {
		r.plugins[name] = make(map[string]Plugin)
	}

	// Check if version already registered
	if _, exists := r.plugins[name][version]; exists {
		return agentErrors.New(agentErrors.CodeAlreadyExists, "plugin version already registered").
			WithComponent("enhanced_plugin_registry").
			WithOperation("register_plugin").
			WithContext("name", name).
			WithContext("version", version)
	}

	// Register plugin
	r.plugins[name][version] = plugin

	// Register with lifecycle manager
	priority := 100 // Default priority
	if config != nil {
		err := r.lifecycle.RegisterWithConfig(
			fmt.Sprintf("%s@%s", name, version),
			plugin,
			priority,
			config.Config,
		)
		if err != nil {
			delete(r.plugins[name], version)
			return err
		}
	}

	return nil
}

// GetPlugin retrieves a specific version of a plugin.
func (r *EnhancedPluginRegistry) GetPlugin(name, version string) (Plugin, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	versions, exists := r.plugins[name]
	if !exists {
		return nil, agentErrors.New(agentErrors.CodeNotFound, "plugin not found").
			WithComponent("enhanced_plugin_registry").
			WithOperation("get_plugin").
			WithContext("name", name)
	}

	plugin, exists := versions[version]
	if !exists {
		return nil, agentErrors.New(agentErrors.CodeNotFound, "plugin version not found").
			WithComponent("enhanced_plugin_registry").
			WithOperation("get_plugin").
			WithContext("name", name).
			WithContext("version", version)
	}

	return plugin, nil
}

// GetLatestPlugin retrieves the latest version of a plugin.
func (r *EnhancedPluginRegistry) GetLatestPlugin(name string) (Plugin, string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	versions, exists := r.plugins[name]
	if !exists {
		return nil, "", agentErrors.New(agentErrors.CodeNotFound, "plugin not found").
			WithComponent("enhanced_plugin_registry").
			WithOperation("get_latest_plugin").
			WithContext("name", name)
	}

	// Simple latest version selection (last registered)
	// In production, should use semantic versioning
	var latestVersion string
	var latestPlugin Plugin
	for version, plugin := range versions {
		if latestVersion == "" || version > latestVersion {
			latestVersion = version
			latestPlugin = plugin
		}
	}

	return latestPlugin, latestVersion, nil
}

// ListPlugins returns all registered plugin names.
func (r *EnhancedPluginRegistry) ListPlugins() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.plugins))
	for name := range r.plugins {
		names = append(names, name)
	}
	return names
}

// ListVersions returns all versions of a plugin.
func (r *EnhancedPluginRegistry) ListVersions(name string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	versions, exists := r.plugins[name]
	if !exists {
		return nil
	}

	result := make([]string, 0, len(versions))
	for version := range versions {
		result = append(result, version)
	}
	return result
}

// GetAllTools returns all tools from all registered plugins.
func (r *EnhancedPluginRegistry) GetAllTools() map[string][]DynamicRunnable {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[string][]DynamicRunnable)
	for name, versions := range r.plugins {
		for version, plugin := range versions {
			key := fmt.Sprintf("%s@%s", name, version)
			result[key] = plugin.GetTools()
		}
	}
	return result
}

// GetAllAgents returns all agents from all registered plugins.
func (r *EnhancedPluginRegistry) GetAllAgents() map[string][]DynamicRunnable {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[string][]DynamicRunnable)
	for name, versions := range r.plugins {
		for version, plugin := range versions {
			key := fmt.Sprintf("%s@%s", name, version)
			result[key] = plugin.GetAgents()
		}
	}
	return result
}

// InitializeAll initializes all plugins in dependency order.
func (r *EnhancedPluginRegistry) InitializeAll(ctx context.Context) error {
	return r.lifecycle.InitAll(ctx)
}

// StartAll starts all initialized plugins.
func (r *EnhancedPluginRegistry) StartAll(ctx context.Context) error {
	return r.lifecycle.StartAll(ctx)
}

// StopAll stops all running plugins.
func (r *EnhancedPluginRegistry) StopAll(ctx context.Context) error {
	return r.lifecycle.StopAll(ctx)
}

// =============================================================================
// Base Plugin Implementation
// =============================================================================

// BasePlugin provides a default implementation of the Plugin interface.
type BasePlugin struct {
	*BaseLifecycle
	name    string
	version string
	tools   []DynamicRunnable
	agents  []DynamicRunnable
}

// NewBasePlugin creates a new base plugin.
func NewBasePlugin(name, version string) *BasePlugin {
	return &BasePlugin{
		BaseLifecycle: NewBaseLifecycle(name),
		name:          name,
		version:       version,
		tools:         make([]DynamicRunnable, 0),
		agents:        make([]DynamicRunnable, 0),
	}
}

// Name returns the plugin name.
func (p *BasePlugin) Name() string {
	return p.name
}

// Version returns the plugin version.
func (p *BasePlugin) Version() string {
	return p.version
}

// GetTools returns registered tools.
func (p *BasePlugin) GetTools() []DynamicRunnable {
	return p.tools
}

// GetAgents returns registered agents.
func (p *BasePlugin) GetAgents() []DynamicRunnable {
	return p.agents
}

// GetMiddleware returns registered middleware.
func (p *BasePlugin) GetMiddleware() []interface{} {
	return nil
}

// AddTool registers a tool with the plugin.
func (p *BasePlugin) AddTool(tool DynamicRunnable) {
	p.tools = append(p.tools, tool)
}

// AddAgent registers an agent with the plugin.
func (p *BasePlugin) AddAgent(agent DynamicRunnable) {
	p.agents = append(p.agents, agent)
}

// =============================================================================
// Global Enhanced Registry
// =============================================================================

var (
	globalEnhancedRegistry     *EnhancedPluginRegistry
	globalEnhancedRegistryOnce sync.Once
)

// GlobalEnhancedPluginRegistry returns the global enhanced plugin registry.
func GlobalEnhancedPluginRegistry() *EnhancedPluginRegistry {
	globalEnhancedRegistryOnce.Do(func() {
		globalEnhancedRegistry = NewEnhancedPluginRegistry()
	})
	return globalEnhancedRegistry
}
