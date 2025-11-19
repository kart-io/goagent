// Package store defines constants used for storage and persistence systems.
package store

// Store Types define the available storage backend types.
const (
	// StoreTypeMemory represents in-memory storage
	StoreTypeMemory = "memory"
	// StoreTypeRedis represents Redis storage
	StoreTypeRedis = "redis"
	// StoreTypePostgres represents PostgreSQL storage
	StoreTypePostgres = "postgres"
	// StoreTypeMySQL represents MySQL storage
	StoreTypeMySQL = "mysql"
	// StoreTypeSQLite represents SQLite storage
	StoreTypeSQLite = "sqlite"
	// StoreTypeMongoDB represents MongoDB storage
	StoreTypeMongoDB = "mongodb"
	// StoreTypeFile represents file-based storage
	StoreTypeFile = "file"
	// StoreTypeS3 represents AWS S3 storage
	StoreTypeS3 = "s3"
)

// Store Operations define common storage operations.
const (
	// OpGet represents a get/read operation
	OpGet = "get"
	// OpSet represents a set/write operation
	OpSet = "set"
	// OpDelete represents a delete operation
	OpDelete = "delete"
	// OpList represents a list operation
	OpList = "list"
	// OpUpdate represents an update operation
	OpUpdate = "update"
	// OpExists represents an exists check operation
	OpExists = "exists"
	// OpClear represents a clear/flush operation
	OpClear = "clear"
)

// Database Field Names
const (
	// FieldKey represents a database key field
	FieldKey = "key"
	// FieldValue represents a database value field
	FieldValue = "value"
	// FieldNamespace represents a namespace field
	FieldNamespace = "namespace"
	// FieldExpiry represents an expiration time field
	FieldExpiry = "expiry"
	// FieldTTL represents time-to-live field
	FieldTTL = "ttl"
	// FieldCreatedAt represents creation timestamp
	FieldCreatedAt = "created_at"
	// FieldUpdatedAt represents update timestamp
	FieldUpdatedAt = "updated_at"
)

// Connection Pool Constants
const (
	// DefaultMaxConnections is the default maximum number of connections
	DefaultMaxConnections = 10
	// DefaultMinConnections is the default minimum number of connections
	DefaultMinConnections = 2
	// DefaultConnectionTimeout is the default connection timeout in seconds
	DefaultConnectionTimeout = 30
	// DefaultIdleTimeout is the default idle connection timeout in seconds
	DefaultIdleTimeout = 300
)

// Redis-specific Constants
const (
	// RedisDefaultDB is the default Redis database number
	RedisDefaultDB = 0
	// RedisDefaultPort is the default Redis port
	RedisDefaultPort = 6379
	// RedisDefaultMaxRetries is the default maximum retry attempts
	RedisDefaultMaxRetries = 3
)

// PostgreSQL-specific Constants
const (
	// PostgresDefaultPort is the default PostgreSQL port
	PostgresDefaultPort = 5432
	// PostgresDefaultSSLMode is the default SSL mode
	PostgresDefaultSSLMode = "disable"
)

// Transaction States
const (
	// TxStateActive represents an active transaction
	TxStateActive = "active"
	// TxStateCommitted represents a committed transaction
	TxStateCommitted = "committed"
	// TxStateRolledBack represents a rolled back transaction
	TxStateRolledBack = "rolled_back"
	// TxStateFailed represents a failed transaction
	TxStateFailed = "failed"
)

// Serialization Formats
const (
	// SerializationJSON represents JSON serialization
	SerializationJSON = "json"
	// SerializationMsgpack represents MessagePack serialization
	SerializationMsgpack = "msgpack"
	// SerializationGob represents Go's gob serialization
	SerializationGob = "gob"
	// SerializationProtobuf represents Protocol Buffers serialization
	SerializationProtobuf = "protobuf"
)

// Error Types
const (
	// ErrTypeConnection represents a connection error
	ErrTypeConnection = "connection_error"
	// ErrTypeTimeout represents a timeout error
	ErrTypeTimeout = "timeout_error"
	// ErrTypeNotFound represents a not found error
	ErrTypeNotFound = "not_found"
	// ErrTypeDuplicate represents a duplicate key error
	ErrTypeDuplicate = "duplicate_error"
	// ErrTypeSerialization represents a serialization error
	ErrTypeSerialization = "serialization_error"
	// ErrTypeTransaction represents a transaction error
	ErrTypeTransaction = "transaction_error"
)
