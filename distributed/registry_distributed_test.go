package distributed

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestRegistry_Register_Success tests successful registration
func TestRegistry_Register_Success(t *testing.T) {
	log := createTestLoggerRegistry()
	registry := NewRegistry(log)

	instance := &ServiceInstance{
		ID:          "instance-1",
		ServiceName: "test-service",
		Endpoint:    "http://localhost:8080",
		Agents:      []string{"Agent1", "Agent2"},
	}

	err := registry.Register(instance)

	assert.NoError(t, err)
	assert.True(t, instance.Healthy)
	assert.NotZero(t, instance.RegisterAt)
	assert.NotZero(t, instance.LastSeen)
}

// TestRegistry_Register_MissingID tests registration with missing ID
func TestRegistry_Register_MissingID(t *testing.T) {
	log := createTestLoggerRegistry() // This will be replaced below
	registry := NewRegistry(log)

	instance := &ServiceInstance{
		ServiceName: "test-service",
		Endpoint:    "http://localhost:8080",
	}

	err := registry.Register(instance)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "instance ID is required")
}

// TestRegistry_Register_MissingServiceName tests registration with missing service name
func TestRegistry_Register_MissingServiceName(t *testing.T) {
	log := createTestLoggerRegistry()
	registry := NewRegistry(log)

	instance := &ServiceInstance{
		ID:       "instance-1",
		Endpoint: "http://localhost:8080",
	}

	err := registry.Register(instance)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "service name is required")
}

// TestRegistry_Register_MissingEndpoint tests registration with missing endpoint
func TestRegistry_Register_MissingEndpoint(t *testing.T) {
	log := createTestLoggerRegistry()
	registry := NewRegistry(log)

	instance := &ServiceInstance{
		ID:          "instance-1",
		ServiceName: "test-service",
	}

	err := registry.Register(instance)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "endpoint is required")
}

// TestRegistry_Register_MultipleInstances tests registering multiple instances
func TestRegistry_Register_MultipleInstances(t *testing.T) {
	log := createTestLoggerRegistry()
	registry := NewRegistry(log)

	for i := 1; i <= 3; i++ {
		instance := &ServiceInstance{
			ID:          "instance-" + string(rune('0'+i)),
			ServiceName: "test-service",
			Endpoint:    "http://localhost:808" + string(rune('0'+i)),
			Agents:      []string{"Agent1"},
		}
		err := registry.Register(instance)
		assert.NoError(t, err)
	}

	instances, err := registry.GetHealthyInstances("test-service")
	assert.NoError(t, err)
	assert.Len(t, instances, 3)
}

// TestRegistry_Deregister_Success tests successful deregistration
func TestRegistry_Deregister_Success(t *testing.T) {
	log := createTestLoggerRegistry()
	registry := NewRegistry(log)

	instance := &ServiceInstance{
		ID:          "instance-1",
		ServiceName: "test-service",
		Endpoint:    "http://localhost:8080",
	}

	err := registry.Register(instance)
	assert.NoError(t, err)

	err = registry.Deregister("instance-1")
	assert.NoError(t, err)

	_, err = registry.GetInstance("instance-1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "instance not found")
}

// TestRegistry_Deregister_NotFound tests deregistration of non-existent instance
func TestRegistry_Deregister_NotFound(t *testing.T) {
	log := createTestLoggerRegistry()
	registry := NewRegistry(log)

	err := registry.Deregister("non-existent")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "instance not found")
}

// TestRegistry_Deregister_RemovesFromService tests that deregistration removes from service
func TestRegistry_Deregister_RemovesFromService(t *testing.T) {
	log := createTestLoggerRegistry()
	registry := NewRegistry(log)

	for i := 1; i <= 3; i++ {
		instance := &ServiceInstance{
			ID:          "instance-" + string(rune('0'+i)),
			ServiceName: "test-service",
			Endpoint:    "http://localhost:808" + string(rune('0'+i)),
		}
		_ = registry.Register(instance)
	}

	err := registry.Deregister("instance-2")
	assert.NoError(t, err)

	instances, err := registry.GetHealthyInstances("test-service")
	assert.NoError(t, err)
	assert.Len(t, instances, 2)

	for _, inst := range instances {
		assert.NotEqual(t, "instance-2", inst.ID)
	}
}

// TestRegistry_Heartbeat_Success tests successful heartbeat
func TestRegistry_Heartbeat_Success(t *testing.T) {
	log := createTestLoggerRegistry()
	registry := NewRegistry(log)

	instance := &ServiceInstance{
		ID:          "instance-1",
		ServiceName: "test-service",
		Endpoint:    "http://localhost:8080",
	}

	err := registry.Register(instance)
	assert.NoError(t, err)

	originalLastSeen := instance.LastSeen
	time.Sleep(10 * time.Millisecond)

	err = registry.Heartbeat("instance-1")
	assert.NoError(t, err)

	instance, err = registry.GetInstance("instance-1")
	assert.NoError(t, err)
	assert.True(t, instance.Healthy)
	assert.True(t, instance.LastSeen.After(originalLastSeen))
}

// TestRegistry_Heartbeat_NotFound tests heartbeat for non-existent instance
func TestRegistry_Heartbeat_NotFound(t *testing.T) {
	log := createTestLoggerRegistry()
	registry := NewRegistry(log)

	err := registry.Heartbeat("non-existent")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "instance not found")
}

// TestRegistry_GetInstance_Success tests retrieving an instance
func TestRegistry_GetInstance_Success(t *testing.T) {
	log := createTestLoggerRegistry()
	registry := NewRegistry(log)

	originalInstance := &ServiceInstance{
		ID:          "instance-1",
		ServiceName: "test-service",
		Endpoint:    "http://localhost:8080",
		Agents:      []string{"Agent1"},
	}

	err := registry.Register(originalInstance)
	assert.NoError(t, err)

	retrievedInstance, err := registry.GetInstance("instance-1")
	assert.NoError(t, err)
	assert.Equal(t, originalInstance.ID, retrievedInstance.ID)
	assert.Equal(t, originalInstance.ServiceName, retrievedInstance.ServiceName)
}

// TestRegistry_GetInstance_NotFound tests retrieving non-existent instance
func TestRegistry_GetInstance_NotFound(t *testing.T) {
	log := createTestLoggerRegistry()
	registry := NewRegistry(log)

	_, err := registry.GetInstance("non-existent")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "instance not found")
}

// TestRegistry_GetHealthyInstances_Success tests retrieving healthy instances
func TestRegistry_GetHealthyInstances_Success(t *testing.T) {
	log := createTestLoggerRegistry()
	registry := NewRegistry(log)

	for i := 1; i <= 3; i++ {
		instance := &ServiceInstance{
			ID:          "instance-" + string(rune('0'+i)),
			ServiceName: "test-service",
			Endpoint:    "http://localhost:808" + string(rune('0'+i)),
		}
		_ = registry.Register(instance)
	}

	instances, err := registry.GetHealthyInstances("test-service")
	assert.NoError(t, err)
	assert.Len(t, instances, 3)

	for _, inst := range instances {
		assert.True(t, inst.Healthy)
	}
}

// TestRegistry_GetHealthyInstances_FilterUnhealthy tests filtering unhealthy instances
func TestRegistry_GetHealthyInstances_FilterUnhealthy(t *testing.T) {
	log := createTestLoggerRegistry()
	registry := NewRegistry(log)

	for i := 1; i <= 3; i++ {
		instance := &ServiceInstance{
			ID:          "instance-" + string(rune('0'+i)),
			ServiceName: "test-service",
			Endpoint:    "http://localhost:808" + string(rune('0'+i)),
		}
		_ = registry.Register(instance)
	}

	registry.MarkUnhealthy("instance-2")

	instances, err := registry.GetHealthyInstances("test-service")
	assert.NoError(t, err)
	assert.Len(t, instances, 2)

	for _, inst := range instances {
		assert.NotEqual(t, "instance-2", inst.ID)
	}
}

// TestRegistry_GetHealthyInstances_ServiceNotFound tests retrieving from non-existent service
func TestRegistry_GetHealthyInstances_ServiceNotFound(t *testing.T) {
	log := createTestLoggerRegistry()
	registry := NewRegistry(log)

	_, err := registry.GetHealthyInstances("non-existent-service")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "service not found")
}

// TestRegistry_GetAllInstances_Success tests retrieving all instances
func TestRegistry_GetAllInstances_Success(t *testing.T) {
	log := createTestLoggerRegistry()
	registry := NewRegistry(log)

	for i := 1; i <= 3; i++ {
		instance := &ServiceInstance{
			ID:          "instance-" + string(rune('0'+i)),
			ServiceName: "test-service",
			Endpoint:    "http://localhost:808" + string(rune('0'+i)),
		}
		_ = registry.Register(instance)
	}

	instances, err := registry.GetAllInstances("test-service")
	assert.NoError(t, err)
	assert.Len(t, instances, 3)
}

// TestRegistry_GetAllInstances_IncludesUnhealthy tests that GetAllInstances includes unhealthy
func TestRegistry_GetAllInstances_IncludesUnhealthy(t *testing.T) {
	log := createTestLoggerRegistry()
	registry := NewRegistry(log)

	for i := 1; i <= 3; i++ {
		instance := &ServiceInstance{
			ID:          "instance-" + string(rune('0'+i)),
			ServiceName: "test-service",
			Endpoint:    "http://localhost:808" + string(rune('0'+i)),
		}
		_ = registry.Register(instance)
	}

	registry.MarkUnhealthy("instance-2")

	instances, err := registry.GetAllInstances("test-service")
	assert.NoError(t, err)
	assert.Len(t, instances, 3)
}

// TestRegistry_GetAllInstances_ServiceNotFound tests retrieving from non-existent service
func TestRegistry_GetAllInstances_ServiceNotFound(t *testing.T) {
	log := createTestLoggerRegistry()
	registry := NewRegistry(log)

	_, err := registry.GetAllInstances("non-existent-service")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "service not found")
}

// TestRegistry_ListServices_Success tests listing all services
func TestRegistry_ListServices_Success(t *testing.T) {
	log := createTestLoggerRegistry()
	registry := NewRegistry(log)

	for serviceIdx := 1; serviceIdx <= 3; serviceIdx++ {
		instance := &ServiceInstance{
			ID:          "instance-" + string(rune('0'+serviceIdx)),
			ServiceName: "service-" + string(rune('0'+serviceIdx)),
			Endpoint:    "http://localhost:808" + string(rune('0'+serviceIdx)),
		}
		_ = registry.Register(instance)
	}

	services := registry.ListServices()
	assert.Len(t, services, 3)
}

// TestRegistry_ListServices_Empty tests listing services when empty
func TestRegistry_ListServices_Empty(t *testing.T) {
	log := createTestLoggerRegistry()
	registry := NewRegistry(log)

	services := registry.ListServices()
	assert.Len(t, services, 0)
}

// TestRegistry_MarkHealthy_Success tests marking instance as healthy
func TestRegistry_MarkHealthy_Success(t *testing.T) {
	log := createTestLoggerRegistry()
	registry := NewRegistry(log)

	instance := &ServiceInstance{
		ID:          "instance-1",
		ServiceName: "test-service",
		Endpoint:    "http://localhost:8080",
	}

	err := registry.Register(instance)
	assert.NoError(t, err)

	registry.MarkUnhealthy("instance-1")
	instance, err = registry.GetInstance("instance-1")
	assert.NoError(t, err)
	assert.False(t, instance.Healthy)

	registry.MarkHealthy("instance-1")
	instance, err = registry.GetInstance("instance-1")
	assert.NoError(t, err)
	assert.True(t, instance.Healthy)
}

// TestRegistry_MarkHealthy_NonExistent tests marking non-existent instance
func TestRegistry_MarkHealthy_NonExistent(t *testing.T) {
	log := createTestLoggerRegistry()
	registry := NewRegistry(log)

	// Should not panic
	registry.MarkHealthy("non-existent")
}

// TestRegistry_MarkUnhealthy_Success tests marking instance as unhealthy
func TestRegistry_MarkUnhealthy_Success(t *testing.T) {
	log := createTestLoggerRegistry()
	registry := NewRegistry(log)

	instance := &ServiceInstance{
		ID:          "instance-1",
		ServiceName: "test-service",
		Endpoint:    "http://localhost:8080",
	}

	err := registry.Register(instance)
	assert.NoError(t, err)
	assert.True(t, instance.Healthy)

	registry.MarkUnhealthy("instance-1")
	instance, err = registry.GetInstance("instance-1")
	assert.NoError(t, err)
	assert.False(t, instance.Healthy)
}

// TestRegistry_MarkUnhealthy_NonExistent tests marking non-existent instance as unhealthy
func TestRegistry_MarkUnhealthy_NonExistent(t *testing.T) {
	log := createTestLoggerRegistry()
	registry := NewRegistry(log)

	// Should not panic
	registry.MarkUnhealthy("non-existent")
}

// TestRegistry_PerformHealthCheck_MarkUnhealthy tests health check timeout mechanism
func TestRegistry_PerformHealthCheck_MarkUnhealthy(t *testing.T) {
	log := createTestLoggerRegistry()
	registry := NewRegistry(log)

	instance := &ServiceInstance{
		ID:          "instance-1",
		ServiceName: "test-service",
		Endpoint:    "http://localhost:8080",
	}

	err := registry.Register(instance)
	assert.NoError(t, err)

	// Manually set LastSeen to far past to trigger timeout
	registry.mu.Lock()
	registry.instances["instance-1"].LastSeen = time.Now().Add(-61 * time.Second)
	registry.mu.Unlock()

	// Call performHealthCheck directly
	registry.performHealthCheck()

	instance, _ = registry.GetInstance("instance-1")
	assert.False(t, instance.Healthy)
}

// TestRegistry_GetStatistics_Success tests statistics retrieval
func TestRegistry_GetStatistics_Success(t *testing.T) {
	log := createTestLoggerRegistry()
	registry := NewRegistry(log)

	for i := 1; i <= 3; i++ {
		instance := &ServiceInstance{
			ID:          "instance-" + string(rune('0'+i)),
			ServiceName: "test-service",
			Endpoint:    "http://localhost:808" + string(rune('0'+i)),
		}
		_ = registry.Register(instance)
	}

	stats := registry.GetStatistics()

	assert.NotNil(t, stats)
	assert.Equal(t, 3, stats["total_instances"])
	assert.Equal(t, 3, stats["healthy_instances"])
	assert.Equal(t, 1, stats["services"])
}

// TestRegistry_GetStatistics_WithUnhealthy tests statistics with unhealthy instances
func TestRegistry_GetStatistics_WithUnhealthy(t *testing.T) {
	log := createTestLoggerRegistry()
	registry := NewRegistry(log)

	for i := 1; i <= 3; i++ {
		instance := &ServiceInstance{
			ID:          "instance-" + string(rune('0'+i)),
			ServiceName: "test-service",
			Endpoint:    "http://localhost:808" + string(rune('0'+i)),
		}
		_ = registry.Register(instance)
	}

	registry.MarkUnhealthy("instance-2")

	stats := registry.GetStatistics()

	assert.Equal(t, 3, stats["total_instances"])
	assert.Equal(t, 2, stats["healthy_instances"])

	serviceStats := stats["service_stats"].(map[string]interface{})
	testServiceStats := serviceStats["test-service"].(map[string]interface{})
	assert.Equal(t, 3, testServiceStats["total"])
	assert.Equal(t, 2, testServiceStats["healthy"])
}

// TestRegistry_GetStatistics_MultipleServices tests statistics with multiple services
func TestRegistry_GetStatistics_MultipleServices(t *testing.T) {
	log := createTestLoggerRegistry()
	registry := NewRegistry(log)

	// Register instances for multiple services
	serviceNames := []string{"service-1", "service-2", "service-3"}
	for idx, serviceName := range serviceNames {
		for i := 1; i <= 2; i++ {
			instance := &ServiceInstance{
				ID:          "instance-" + string(rune('0'+idx)) + "-" + string(rune('0'+i)),
				ServiceName: serviceName,
				Endpoint:    "http://localhost:808" + string(rune('0'+i)),
			}
			_ = registry.Register(instance)
		}
	}

	stats := registry.GetStatistics()

	assert.Equal(t, 6, stats["total_instances"])
	assert.Equal(t, 6, stats["healthy_instances"])
	assert.Equal(t, 3, stats["services"])
}

// TestRegistry_ConcurrentOperations tests concurrent access to registry
func TestRegistry_ConcurrentOperations(t *testing.T) {
	log := createTestLoggerRegistry()
	registry := NewRegistry(log)

	var wg sync.WaitGroup
	numGoroutines := 10
	numInstancesPerGoroutine := 5

	// Register instances concurrently
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for j := 0; j < numInstancesPerGoroutine; j++ {
				instance := &ServiceInstance{
					ID:          "instance-" + string(rune('0'+goroutineID)) + "-" + string(rune('0'+j)),
					ServiceName: "test-service",
					Endpoint:    "http://localhost:8080",
				}
				_ = registry.Register(instance)
			}
		}(i)
	}

	// Perform operations concurrently
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = registry.GetHealthyInstances("test-service")
			_ = registry.ListServices()
			_ = registry.GetStatistics()
		}()
	}

	wg.Wait()

	stats := registry.GetStatistics()
	assert.Equal(t, numGoroutines*numInstancesPerGoroutine, stats["total_instances"])
}

// TestRegistry_Metadata tests instance with metadata
func TestRegistry_Metadata(t *testing.T) {
	log := createTestLoggerRegistry()
	registry := NewRegistry(log)

	instance := &ServiceInstance{
		ID:          "instance-1",
		ServiceName: "test-service",
		Endpoint:    "http://localhost:8080",
		Metadata: map[string]interface{}{
			"version": "1.0.0",
			"region":  "us-west",
			"zone":    "zone-a",
		},
	}

	err := registry.Register(instance)
	assert.NoError(t, err)

	retrieved, err := registry.GetInstance("instance-1")
	assert.NoError(t, err)
	assert.Equal(t, "1.0.0", retrieved.Metadata["version"])
	assert.Equal(t, "us-west", retrieved.Metadata["region"])
	assert.Equal(t, "zone-a", retrieved.Metadata["zone"])
}

// TestRegistry_HealthCheckContinuous tests that health check runs continuously
func TestRegistry_HealthCheckContinuous(t *testing.T) {
	log := createTestLoggerRegistry()
	registry := NewRegistry(log)

	instance := &ServiceInstance{
		ID:          "instance-1",
		ServiceName: "test-service",
		Endpoint:    "http://localhost:8080",
	}

	err := registry.Register(instance)
	assert.NoError(t, err)

	// Wait slightly and verify heartbeat updates LastSeen
	time.Sleep(100 * time.Millisecond)

	_ = registry.Heartbeat("instance-1")
	instance1, _ := registry.GetInstance("instance-1")
	lastSeen1 := instance1.LastSeen

	time.Sleep(100 * time.Millisecond)

	_ = registry.Heartbeat("instance-1")
	instance2, _ := registry.GetInstance("instance-1")
	lastSeen2 := instance2.LastSeen

	assert.True(t, lastSeen2.After(lastSeen1))
}
