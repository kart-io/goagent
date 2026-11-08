package constants

import "time"

// 共享默认策略与协议值；领域实现直接引用本包。
const (
	CheckpointReplicationModeSync        = "sync"
	CheckpointReplicationModeAsync       = "async"
	DefaultCheckpointHealthCheckInterval = 30 * time.Second
	DefaultCheckpointMaxFailoverAttempts = 3
)
