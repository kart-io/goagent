package distributed

import (
	"fmt"
	"sync"
	"time"

	"github.com/kart-io/logger/core"
)

// Registry Agent 注册中心
// 管理所有服务实例的注册信息
type Registry struct {
	mu        sync.RWMutex
	services  map[string][]*ServiceInstance // serviceName -> instances
	instances map[string]*ServiceInstance   // instanceID -> instance
	logger    core.Logger
}

// ServiceInstance 服务实例
type ServiceInstance struct {
	ID          string                 // 实例 ID
	ServiceName string                 // 服务名称
	Endpoint    string                 // 服务端点 (e.g., http://localhost:8080)
	Agents      []string               // 支持的 Agent 列表
	Metadata    map[string]interface{} // 元数据
	RegisterAt  time.Time              // 注册时间
	LastSeen    time.Time              // 最后心跳时间
	Healthy     bool                   // 健康状态
}

// NewRegistry 创建注册中心
func NewRegistry(logger core.Logger) *Registry {
	r := &Registry{
		services:  make(map[string][]*ServiceInstance),
		instances: make(map[string]*ServiceInstance),
		logger:    logger.With("component", "agent-registry"),
	}

	// 启动健康检查
	go r.healthCheck()

	return r
}

// Register 注册服务实例
func (r *Registry) Register(instance *ServiceInstance) error {
	if instance.ID == "" {
		return fmt.Errorf("instance ID is required")
	}

	if instance.ServiceName == "" {
		return fmt.Errorf("service name is required")
	}

	if instance.Endpoint == "" {
		return fmt.Errorf("endpoint is required")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	instance.RegisterAt = now
	instance.LastSeen = now
	instance.Healthy = true

	// 保存实例
	r.instances[instance.ID] = instance

	// 添加到服务列表
	r.services[instance.ServiceName] = append(r.services[instance.ServiceName], instance)

	r.logger.Info("Service instance registered",
		"instance_id", instance.ID,
		"service", instance.ServiceName,
		"endpoint", instance.Endpoint,
		"agents", len(instance.Agents))

	return nil
}

// Deregister 注销服务实例
func (r *Registry) Deregister(instanceID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	instance, ok := r.instances[instanceID]
	if !ok {
		return fmt.Errorf("instance not found: %s", instanceID)
	}

	// 从服务列表中移除
	instances := r.services[instance.ServiceName]
	newInstances := make([]*ServiceInstance, 0, len(instances)-1)
	for _, inst := range instances {
		if inst.ID != instanceID {
			newInstances = append(newInstances, inst)
		}
	}
	r.services[instance.ServiceName] = newInstances

	// 删除实例
	delete(r.instances, instanceID)

	r.logger.Info("Service instance deregistered",
		"instance_id", instanceID,
		"service", instance.ServiceName)

	return nil
}

// Heartbeat 更新实例心跳
func (r *Registry) Heartbeat(instanceID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	instance, ok := r.instances[instanceID]
	if !ok {
		return fmt.Errorf("instance not found: %s", instanceID)
	}

	instance.LastSeen = time.Now()
	instance.Healthy = true

	return nil
}

// GetInstance 获取实例
func (r *Registry) GetInstance(instanceID string) (*ServiceInstance, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	instance, ok := r.instances[instanceID]
	if !ok {
		return nil, fmt.Errorf("instance not found: %s", instanceID)
	}

	return instance, nil
}

// GetHealthyInstances 获取健康的服务实例
func (r *Registry) GetHealthyInstances(serviceName string) ([]*ServiceInstance, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	instances, ok := r.services[serviceName]
	if !ok {
		return nil, fmt.Errorf("service not found: %s", serviceName)
	}

	healthy := make([]*ServiceInstance, 0, len(instances))
	for _, inst := range instances {
		if inst.Healthy {
			healthy = append(healthy, inst)
		}
	}

	return healthy, nil
}

// GetAllInstances 获取所有服务实例
func (r *Registry) GetAllInstances(serviceName string) ([]*ServiceInstance, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	instances, ok := r.services[serviceName]
	if !ok {
		return nil, fmt.Errorf("service not found: %s", serviceName)
	}

	return instances, nil
}

// ListServices 列出所有服务
func (r *Registry) ListServices() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	services := make([]string, 0, len(r.services))
	for name := range r.services {
		services = append(services, name)
	}

	return services
}

// MarkHealthy 标记实例为健康
func (r *Registry) MarkHealthy(instanceID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if instance, ok := r.instances[instanceID]; ok {
		instance.Healthy = true
		instance.LastSeen = time.Now()
	}
}

// MarkUnhealthy 标记实例为不健康
func (r *Registry) MarkUnhealthy(instanceID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if instance, ok := r.instances[instanceID]; ok {
		instance.Healthy = false
		r.logger.Warnw("Instance marked as unhealthy",
			"instance_id", instanceID,
			"service", instance.ServiceName)
	}
}

// healthCheck 定期健康检查
func (r *Registry) healthCheck() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		r.performHealthCheck()
	}
}

// performHealthCheck 执行健康检查
func (r *Registry) performHealthCheck() {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	timeout := 60 * time.Second

	for id, instance := range r.instances {
		if now.Sub(instance.LastSeen) > timeout {
			instance.Healthy = false
			r.logger.Warnw("Instance marked as unhealthy due to timeout",
				"instance_id", id,
				"service", instance.ServiceName,
				"last_seen", instance.LastSeen)
		}
	}
}

// GetStatistics 获取统计信息
func (r *Registry) GetStatistics() map[string]interface{} {
	r.mu.RLock()
	defer r.mu.RUnlock()

	totalInstances := len(r.instances)
	healthyInstances := 0
	for _, inst := range r.instances {
		if inst.Healthy {
			healthyInstances++
		}
	}

	serviceStats := make(map[string]interface{})
	for name, instances := range r.services {
		healthy := 0
		for _, inst := range instances {
			if inst.Healthy {
				healthy++
			}
		}
		serviceStats[name] = map[string]interface{}{
			"total":   len(instances),
			"healthy": healthy,
		}
	}

	return map[string]interface{}{
		"total_instances":   totalInstances,
		"healthy_instances": healthyInstances,
		"services":          len(r.services),
		"service_stats":     serviceStats,
	}
}
