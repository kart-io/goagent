package toolbox

import (
	"sync"
	"time"

	"github.com/kart-io/goagent/mcp/core"
)

// PermissionManager 权限管理器
type PermissionManager struct {
	// 权限规则
	permissions map[string]map[string]*core.ToolPermission // userID -> toolName -> permission
	mutex       sync.RWMutex

	// 速率限制跟踪
	rateLimitTracking map[string]map[string]*rateLimitInfo // userID -> toolName -> info
	rateMutex         sync.RWMutex

	// 默认权限策略
	defaultAllowAll       bool
	defaultAllowDangerous bool
}

// rateLimitInfo 速率限制信息
type rateLimitInfo struct {
	calls     int
	resetTime time.Time
}

// NewPermissionManager 创建权限管理器
func NewPermissionManager() *PermissionManager {
	return &PermissionManager{
		permissions:           make(map[string]map[string]*core.ToolPermission),
		rateLimitTracking:     make(map[string]map[string]*rateLimitInfo),
		defaultAllowAll:       true,  // 默认允许所有工具
		defaultAllowDangerous: false, // 默认不允许危险操作
	}
}

// SetPermission 设置权限
func (pm *PermissionManager) SetPermission(perm *core.ToolPermission) {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()

	if _, exists := pm.permissions[perm.UserID]; !exists {
		pm.permissions[perm.UserID] = make(map[string]*core.ToolPermission)
	}

	pm.permissions[perm.UserID][perm.ToolName] = perm
}

// GetPermission 获取权限
func (pm *PermissionManager) GetPermission(userID, toolName string) (*core.ToolPermission, bool) {
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()

	if userPerms, exists := pm.permissions[userID]; exists {
		if perm, exists := userPerms[toolName]; exists {
			return perm, true
		}
	}

	return nil, false
}

// HasPermission 检查是否有权限
func (pm *PermissionManager) HasPermission(userID, toolName string) (bool, error) {
	// 如果没有指定用户，使用默认策略
	if userID == "" {
		return pm.defaultAllowAll, nil
	}

	pm.mutex.RLock()
	perm, exists := pm.getPermissionLocked(userID, toolName)
	pm.mutex.RUnlock()

	// 如果没有明确的权限设置，使用默认策略
	if !exists {
		return pm.defaultAllowAll, nil
	}

	// 检查是否允许
	if !perm.Allowed {
		return false, &core.ErrPermissionDenied{
			UserID:   userID,
			ToolName: toolName,
			Reason:   perm.Reason,
		}
	}

	// 检查速率限制
	if perm.MaxCallsPerMinute > 0 {
		allowed := pm.checkRateLimit(userID, toolName, perm.MaxCallsPerMinute)
		if !allowed {
			return false, &core.ErrPermissionDenied{
				UserID:   userID,
				ToolName: toolName,
				Reason:   "rate limit exceeded",
			}
		}
	}

	return true, nil
}

// getPermissionLocked 获取权限（已加锁）
func (pm *PermissionManager) getPermissionLocked(userID, toolName string) (*core.ToolPermission, bool) {
	if userPerms, exists := pm.permissions[userID]; exists {
		if perm, exists := userPerms[toolName]; exists {
			return perm, true
		}
	}
	return nil, false
}

// checkRateLimit 检查速率限制
func (pm *PermissionManager) checkRateLimit(userID, toolName string, maxCalls int) bool {
	pm.rateMutex.Lock()
	defer pm.rateMutex.Unlock()

	now := time.Now()

	// 初始化跟踪
	if _, exists := pm.rateLimitTracking[userID]; !exists {
		pm.rateLimitTracking[userID] = make(map[string]*rateLimitInfo)
	}

	info, exists := pm.rateLimitTracking[userID][toolName]
	if !exists || now.After(info.resetTime) {
		// 创建新的限制周期
		pm.rateLimitTracking[userID][toolName] = &rateLimitInfo{
			calls:     1,
			resetTime: now.Add(time.Minute),
		}
		return true
	}

	// 检查是否超过限制
	if info.calls >= maxCalls {
		return false
	}

	// 增加调用次数
	info.calls++
	return true
}

// RevokePermission 撤销权限
func (pm *PermissionManager) RevokePermission(userID, toolName string) {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()

	if userPerms, exists := pm.permissions[userID]; exists {
		delete(userPerms, toolName)
	}
}

// RevokeAllUserPermissions 撤销用户的所有权限
func (pm *PermissionManager) RevokeAllUserPermissions(userID string) {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()

	delete(pm.permissions, userID)
}

// GrantAll 授予所有工具权限
func (pm *PermissionManager) GrantAll(userID string, allowDangerous bool) {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()

	if _, exists := pm.permissions[userID]; !exists {
		pm.permissions[userID] = make(map[string]*core.ToolPermission)
	}

	pm.permissions[userID]["*"] = &core.ToolPermission{
		UserID:            userID,
		ToolName:          "*",
		Allowed:           true,
		AllowDangerousOps: allowDangerous,
		Reason:            "granted all tools access",
	}
}

// DenyAll 拒绝所有工具权限
func (pm *PermissionManager) DenyAll(userID string) {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()

	if _, exists := pm.permissions[userID]; !exists {
		pm.permissions[userID] = make(map[string]*core.ToolPermission)
	}

	pm.permissions[userID]["*"] = &core.ToolPermission{
		UserID:   userID,
		ToolName: "*",
		Allowed:  false,
		Reason:   "denied all tools access",
	}
}

// ListUserPermissions 列出用户的所有权限
func (pm *PermissionManager) ListUserPermissions(userID string) []*core.ToolPermission {
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()

	perms := make([]*core.ToolPermission, 0)
	if userPerms, exists := pm.permissions[userID]; exists {
		for _, perm := range userPerms {
			perms = append(perms, perm)
		}
	}

	return perms
}

// SetDefaultPolicy 设置默认策略
func (pm *PermissionManager) SetDefaultPolicy(allowAll, allowDangerous bool) {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()

	pm.defaultAllowAll = allowAll
	pm.defaultAllowDangerous = allowDangerous
}

// ClearRateLimitTracking 清除速率限制跟踪
func (pm *PermissionManager) ClearRateLimitTracking(userID string) {
	pm.rateMutex.Lock()
	defer pm.rateMutex.Unlock()

	if userID == "" {
		pm.rateLimitTracking = make(map[string]map[string]*rateLimitInfo)
	} else {
		delete(pm.rateLimitTracking, userID)
	}
}

// GetRateLimitStatus 获取速率限制状态
func (pm *PermissionManager) GetRateLimitStatus(userID, toolName string) (calls int, resetTime time.Time, exists bool) {
	pm.rateMutex.RLock()
	defer pm.rateMutex.RUnlock()

	if userTracking, ok := pm.rateLimitTracking[userID]; ok {
		if info, ok := userTracking[toolName]; ok {
			return info.calls, info.resetTime, true
		}
	}

	return 0, time.Time{}, false
}
