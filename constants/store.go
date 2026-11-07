// 存储后端、字段、操作与连接默认值。
package constants

const (
	StoreTypeMemory          = "memory"
	StoreTypeRedis           = "redis"
	StoreTypePostgres        = "postgres"
	StoreTypeMySQL           = "mysql"
	StoreTypeSQLite          = "sqlite"
	StoreTypeMongoDB         = "mongodb"
	StoreTypeFile            = "file"
	StoreTypeS3              = "s3"
	OpGet                    = "get"
	OpSet                    = "set"
	OpDelete                 = "delete"
	OpList                   = "list"
	OpUpdate                 = "update"
	OpExists                 = "exists"
	OpClear                  = "clear"
	FieldKey                 = "key"
	FieldValue               = "value"
	FieldExpiry              = "expiry"
	FieldTTL                 = "ttl"
	DefaultMaxConnections    = 10
	DefaultMinConnections    = 2
	DefaultConnectionTimeout = 30
	DefaultIdleTimeout       = 300
	RedisDefaultDB           = 0
	RedisDefaultPort         = 6379
	RedisDefaultMaxRetries   = 3
	PostgresDefaultPort      = 5432
	PostgresDefaultSSLMode   = "disable"
	TxStateActive            = "active"
	TxStateCommitted         = "committed"
	TxStateRolledBack        = "rolled_back"
	TxStateFailed            = "failed"
	SerializationJSON        = "json"
	SerializationMsgpack     = "msgpack"
	SerializationGob         = "gob"
	SerializationProtobuf    = "protobuf"
)
