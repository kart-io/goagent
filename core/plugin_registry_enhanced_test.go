package core

import (
	"context"
	"errors"
	"testing"

	"github.com/kart-io/goagent/interfaces"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Test Fixtures
// =============================================================================

// mockPlugin is a test implementation of Plugin.
type mockPlugin struct {
	*BasePlugin
	initCalled  bool
	startCalled bool
	stopCalled  bool
}

func newMockPlugin(name, version string) *mockPlugin {
	return &mockPlugin{
		BasePlugin: NewBasePlugin(name, version),
	}
}

func (p *mockPlugin) Init(ctx context.Context, config interface{}) error {
	p.initCalled = true
	return nil
}

func (p *mockPlugin) Start(ctx context.Context) error {
	p.startCalled = true
	return nil
}

func (p *mockPlugin) Stop(ctx context.Context) error {
	p.stopCalled = true
	return nil
}

func (p *mockPlugin) HealthCheck(ctx context.Context) interfaces.HealthStatus {
	return interfaces.NewHealthyStatus()
}

// mockService is a test service for dependency injection.
type mockService struct {
	name string
}

// =============================================================================
// Container Tests
// =============================================================================

func TestContainer_Register(t *testing.T) {
	container := NewContainer()
	service := &mockService{name: "test"}

	container.Register("service", service)

	resolved, err := container.Resolve("service")
	require.NoError(t, err)
	assert.Same(t, service, resolved)
}

func TestContainer_RegisterFactory(t *testing.T) {
	container := NewContainer()
	callCount := 0

	container.RegisterFactory("service", func() (interface{}, error) {
		callCount++
		return &mockService{name: "factory"}, nil
	})

	// First resolve - factory called
	service1, err := container.Resolve("service")
	require.NoError(t, err)
	assert.Equal(t, 1, callCount)

	// Second resolve - cached instance returned
	service2, err := container.Resolve("service")
	require.NoError(t, err)
	assert.Equal(t, 1, callCount) // Factory not called again
	assert.Same(t, service1, service2)
}

func TestContainer_ResolveTyped(t *testing.T) {
	container := NewContainer()
	service := &mockService{name: "typed"}
	container.Register("service", service)

	t.Run("Correct type", func(t *testing.T) {
		typed, err := ResolveTyped[*mockService](container, "service")
		require.NoError(t, err)
		assert.Same(t, service, typed)
	})

	t.Run("Wrong type", func(t *testing.T) {
		_, err := ResolveTyped[string](container, "service")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "type mismatch")
	})
}

func TestContainer_NotFound(t *testing.T) {
	container := NewContainer()

	_, err := container.Resolve("nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestContainer_FactoryError(t *testing.T) {
	container := NewContainer()

	container.RegisterFactory("failing", func() (interface{}, error) {
		return nil, errors.New("factory failed")
	})

	_, err := container.Resolve("failing")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "factory failed")
}

// =============================================================================
// Enhanced Plugin Registry Tests
// =============================================================================

func TestEnhancedPluginRegistry_RegisterPlugin(t *testing.T) {
	registry := NewEnhancedPluginRegistry()
	plugin := newMockPlugin("test-plugin", "1.0.0")

	err := registry.RegisterPlugin(plugin, nil)
	assert.NoError(t, err)
}

func TestEnhancedPluginRegistry_RegisterDuplicateVersion(t *testing.T) {
	registry := NewEnhancedPluginRegistry()
	plugin1 := newMockPlugin("test-plugin", "1.0.0")
	plugin2 := newMockPlugin("test-plugin", "1.0.0")

	err := registry.RegisterPlugin(plugin1, nil)
	require.NoError(t, err)

	err = registry.RegisterPlugin(plugin2, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already registered")
}

func TestEnhancedPluginRegistry_MultipleVersions(t *testing.T) {
	registry := NewEnhancedPluginRegistry()
	plugin1 := newMockPlugin("test-plugin", "1.0.0")
	plugin2 := newMockPlugin("test-plugin", "2.0.0")

	err := registry.RegisterPlugin(plugin1, nil)
	require.NoError(t, err)

	err = registry.RegisterPlugin(plugin2, nil)
	require.NoError(t, err)

	versions := registry.ListVersions("test-plugin")
	assert.Len(t, versions, 2)
	assert.Contains(t, versions, "1.0.0")
	assert.Contains(t, versions, "2.0.0")
}

func TestEnhancedPluginRegistry_GetPlugin(t *testing.T) {
	registry := NewEnhancedPluginRegistry()
	plugin := newMockPlugin("test-plugin", "1.0.0")
	registry.RegisterPlugin(plugin, nil)

	t.Run("Get existing plugin", func(t *testing.T) {
		retrieved, err := registry.GetPlugin("test-plugin", "1.0.0")
		require.NoError(t, err)
		assert.Same(t, plugin, retrieved)
	})

	t.Run("Get non-existent plugin", func(t *testing.T) {
		_, err := registry.GetPlugin("nonexistent", "1.0.0")
		assert.Error(t, err)
	})

	t.Run("Get non-existent version", func(t *testing.T) {
		_, err := registry.GetPlugin("test-plugin", "2.0.0")
		assert.Error(t, err)
	})
}

func TestEnhancedPluginRegistry_GetLatestPlugin(t *testing.T) {
	registry := NewEnhancedPluginRegistry()
	plugin1 := newMockPlugin("test-plugin", "1.0.0")
	plugin2 := newMockPlugin("test-plugin", "2.0.0")
	plugin3 := newMockPlugin("test-plugin", "1.5.0")

	registry.RegisterPlugin(plugin1, nil)
	registry.RegisterPlugin(plugin2, nil)
	registry.RegisterPlugin(plugin3, nil)

	latest, version, err := registry.GetLatestPlugin("test-plugin")
	require.NoError(t, err)
	assert.Equal(t, "2.0.0", version)
	assert.Same(t, plugin2, latest)
}

func TestEnhancedPluginRegistry_ListPlugins(t *testing.T) {
	registry := NewEnhancedPluginRegistry()
	plugin1 := newMockPlugin("plugin-a", "1.0.0")
	plugin2 := newMockPlugin("plugin-b", "1.0.0")
	plugin3 := newMockPlugin("plugin-c", "1.0.0")

	registry.RegisterPlugin(plugin1, nil)
	registry.RegisterPlugin(plugin2, nil)
	registry.RegisterPlugin(plugin3, nil)

	names := registry.ListPlugins()
	assert.Len(t, names, 3)
	assert.Contains(t, names, "plugin-a")
	assert.Contains(t, names, "plugin-b")
	assert.Contains(t, names, "plugin-c")
}

func TestEnhancedPluginRegistry_GetAllTools(t *testing.T) {
	registry := NewEnhancedPluginRegistry()
	plugin := newMockPlugin("test-plugin", "1.0.0")

	// Add mock tools
	tool1 := &TypedToDynamicAdapter[string, int]{}
	tool2 := &TypedToDynamicAdapter[string, string]{}
	plugin.AddTool(tool1)
	plugin.AddTool(tool2)

	registry.RegisterPlugin(plugin, nil)

	tools := registry.GetAllTools()
	assert.Len(t, tools, 1)
	assert.Len(t, tools["test-plugin@1.0.0"], 2)
}

func TestEnhancedPluginRegistry_GetAllAgents(t *testing.T) {
	registry := NewEnhancedPluginRegistry()
	plugin := newMockPlugin("test-plugin", "1.0.0")

	// Add mock agents
	agent1 := &TypedToDynamicAdapter[string, string]{}
	plugin.AddAgent(agent1)

	registry.RegisterPlugin(plugin, nil)

	agents := registry.GetAllAgents()
	assert.Len(t, agents, 1)
	assert.Len(t, agents["test-plugin@1.0.0"], 1)
}

func TestEnhancedPluginRegistry_Lifecycle(t *testing.T) {
	registry := NewEnhancedPluginRegistry()
	plugin := newMockPlugin("test-plugin", "1.0.0")

	config := &PluginConfig{
		Name:    "test-plugin",
		Version: "1.0.0",
		Config:  map[string]interface{}{"key": "value"},
		Enabled: true,
	}

	err := registry.RegisterPlugin(plugin, config)
	require.NoError(t, err)

	// Initialize
	err = registry.InitializeAll(context.Background())
	assert.NoError(t, err)
	assert.True(t, plugin.initCalled)

	// Start
	err = registry.StartAll(context.Background())
	assert.NoError(t, err)
	assert.True(t, plugin.startCalled)

	// Stop
	err = registry.StopAll(context.Background())
	assert.NoError(t, err)
	assert.True(t, plugin.stopCalled)
}

func TestEnhancedPluginRegistry_ContainerIntegration(t *testing.T) {
	registry := NewEnhancedPluginRegistry()

	// Register services in container
	service := &mockService{name: "shared"}
	registry.Container().Register("service", service)

	// Plugin can access container
	container := registry.Container()
	resolved, err := container.Resolve("service")
	require.NoError(t, err)
	assert.Same(t, service, resolved)
}

// =============================================================================
// BasePlugin Tests
// =============================================================================

func TestBasePlugin_Creation(t *testing.T) {
	plugin := NewBasePlugin("test", "1.0.0")

	assert.Equal(t, "test", plugin.Name())
	assert.Equal(t, "1.0.0", plugin.Version())
	assert.Empty(t, plugin.GetTools())
	assert.Empty(t, plugin.GetAgents())
	assert.Nil(t, plugin.GetMiddleware())
}

func TestBasePlugin_AddTool(t *testing.T) {
	plugin := NewBasePlugin("test", "1.0.0")
	tool := &TypedToDynamicAdapter[string, int]{}

	plugin.AddTool(tool)

	tools := plugin.GetTools()
	assert.Len(t, tools, 1)
	assert.Same(t, tool, tools[0])
}

func TestBasePlugin_AddAgent(t *testing.T) {
	plugin := NewBasePlugin("test", "1.0.0")
	agent := &TypedToDynamicAdapter[string, string]{}

	plugin.AddAgent(agent)

	agents := plugin.GetAgents()
	assert.Len(t, agents, 1)
	assert.Same(t, agent, agents[0])
}

// =============================================================================
// Global Registry Tests
// =============================================================================

func TestGlobalEnhancedPluginRegistry(t *testing.T) {
	registry1 := GlobalEnhancedPluginRegistry()
	registry2 := GlobalEnhancedPluginRegistry()

	assert.Same(t, registry1, registry2)
}
