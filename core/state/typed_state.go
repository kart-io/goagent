// Package state provides type-safe state management with schema validation.
//
// This file addresses the state serialization bottleneck problem:
//   - Replaces map[string]interface{} with typed, schema-validated state
//   - Provides contract enforcement for distributed systems
//   - Enables efficient serialization with known schemas
package state

import (
	"fmt"
	"reflect"
	"sync"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/utils/json"
)

// =============================================================================
// Schema Definition System
// =============================================================================

// FieldType 定义字段的数据类型
type FieldType string

const (
	FieldTypeString FieldType = "string"
	FieldTypeInt    FieldType = "int"
	FieldTypeFloat  FieldType = "float"
	FieldTypeBool   FieldType = "bool"
	FieldTypeObject FieldType = "object"
	FieldTypeArray  FieldType = "array"
	FieldTypeAny    FieldType = "any" // 向后兼容
	FieldTypeBytes  FieldType = "bytes"
	FieldTypeTime   FieldType = "time"
)

// FieldSchema 定义单个字段的 Schema
type FieldSchema struct {
	// Name 字段名称
	Name string `json:"name"`

	// Type 字段类型
	Type FieldType `json:"type"`

	// Required 是否必需
	Required bool `json:"required"`

	// Description 字段描述
	Description string `json:"description,omitempty"`

	// Default 默认值
	Default interface{} `json:"default,omitempty"`

	// ItemType 数组元素类型（仅当 Type == FieldTypeArray 时有效）
	ItemType FieldType `json:"item_type,omitempty"`

	// ObjectSchema 嵌套对象的 Schema（仅当 Type == FieldTypeObject 时有效）
	ObjectSchema *StateSchema `json:"object_schema,omitempty"`

	// Validator 自定义验证函数
	Validator func(value interface{}) error `json:"-"`
}

// StateSchema 定义状态的完整 Schema
type StateSchema struct {
	// Name Schema 名称
	Name string `json:"name"`

	// Version Schema 版本（用于兼容性检查）
	Version string `json:"version"`

	// Description Schema 描述
	Description string `json:"description,omitempty"`

	// Fields 字段定义
	Fields map[string]*FieldSchema `json:"fields"`

	// StrictMode 严格模式：不允许未定义的字段
	StrictMode bool `json:"strict_mode"`
}

// NewStateSchema 创建新的 Schema
func NewStateSchema(name, version string) *StateSchema {
	return &StateSchema{
		Name:    name,
		Version: version,
		Fields:  make(map[string]*FieldSchema),
	}
}

// AddField 添加字段定义
func (s *StateSchema) AddField(field *FieldSchema) *StateSchema {
	s.Fields[field.Name] = field
	return s
}

// WithStrictMode 启用严格模式
func (s *StateSchema) WithStrictMode() *StateSchema {
	s.StrictMode = true
	return s
}

// Validate 验证数据是否符合 Schema
func (s *StateSchema) Validate(data map[string]interface{}) error {
	// 检查必需字段
	for name, field := range s.Fields {
		value, exists := data[name]

		if field.Required && !exists {
			return agentErrors.New(agentErrors.CodeStateValidation, "missing required field").
				WithComponent("state_schema").
				WithOperation("validate").
				WithContext("field", name).
				WithContext("schema", s.Name)
		}

		if exists {
			if err := s.validateField(field, value); err != nil {
				return err
			}
		}
	}

	// 严格模式：检查未定义的字段
	if s.StrictMode {
		for key := range data {
			if _, defined := s.Fields[key]; !defined {
				return agentErrors.New(agentErrors.CodeStateValidation, "undefined field in strict mode").
					WithComponent("state_schema").
					WithOperation("validate").
					WithContext("field", key).
					WithContext("schema", s.Name)
			}
		}
	}

	return nil
}

// validateField 验证单个字段
func (s *StateSchema) validateField(field *FieldSchema, value interface{}) error {
	if value == nil {
		return nil
	}

	// 类型检查
	if err := validateType(field.Type, value); err != nil {
		return agentErrors.Wrap(err, agentErrors.CodeStateValidation, "type validation failed").
			WithComponent("state_schema").
			WithOperation("validate_field").
			WithContext("field", field.Name).
			WithContext("expected_type", string(field.Type))
	}

	// 自定义验证
	if field.Validator != nil {
		if err := field.Validator(value); err != nil {
			return agentErrors.Wrap(err, agentErrors.CodeStateValidation, "custom validation failed").
				WithComponent("state_schema").
				WithOperation("validate_field").
				WithContext("field", field.Name)
		}
	}

	// 嵌套对象验证
	if field.Type == FieldTypeObject && field.ObjectSchema != nil {
		if objMap, ok := value.(map[string]interface{}); ok {
			if err := field.ObjectSchema.Validate(objMap); err != nil {
				return err
			}
		}
	}

	return nil
}

// validateType 验证值的类型
func validateType(expected FieldType, value interface{}) error {
	if expected == FieldTypeAny {
		return nil
	}

	v := reflect.ValueOf(value)
	kind := v.Kind()

	switch expected {
	case FieldTypeString:
		if kind != reflect.String {
			return fmt.Errorf("expected string, got %s", kind)
		}
	case FieldTypeInt:
		if kind != reflect.Int && kind != reflect.Int64 && kind != reflect.Int32 &&
			kind != reflect.Float64 { // JSON 数字默认解析为 float64
			return fmt.Errorf("expected int, got %s", kind)
		}
	case FieldTypeFloat:
		if kind != reflect.Float64 && kind != reflect.Float32 {
			return fmt.Errorf("expected float, got %s", kind)
		}
	case FieldTypeBool:
		if kind != reflect.Bool {
			return fmt.Errorf("expected bool, got %s", kind)
		}
	case FieldTypeObject:
		if kind != reflect.Map {
			return fmt.Errorf("expected object, got %s", kind)
		}
	case FieldTypeArray:
		if kind != reflect.Slice && kind != reflect.Array {
			return fmt.Errorf("expected array, got %s", kind)
		}
	}

	return nil
}

// =============================================================================
// Typed State Implementation
// =============================================================================

// TypedState 是带有 Schema 验证的类型安全状态
type TypedState struct {
	mu     sync.RWMutex
	data   map[string]interface{}
	schema *StateSchema
	dirty  bool // 标记是否有未保存的更改
}

// NewTypedState 创建带有 Schema 的类型安全状态
func NewTypedState(schema *StateSchema) *TypedState {
	ts := &TypedState{
		data:   make(map[string]interface{}),
		schema: schema,
	}

	// 应用默认值
	if schema != nil {
		for name, field := range schema.Fields {
			if field.Default != nil {
				ts.data[name] = field.Default
			}
		}
	}

	return ts
}

// Get 获取字段值
func (ts *TypedState) Get(key string) (interface{}, bool) {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	value, exists := ts.data[key]
	return value, exists
}

// GetString 获取字符串字段
func (ts *TypedState) GetString(key string) (string, error) {
	value, exists := ts.Get(key)
	if !exists {
		return "", nil
	}

	if str, ok := value.(string); ok {
		return str, nil
	}

	return "", agentErrors.New(agentErrors.CodeTypeMismatch, "field is not a string").
		WithContext("field", key)
}

// GetInt 获取整数字段
func (ts *TypedState) GetInt(key string) (int64, error) {
	value, exists := ts.Get(key)
	if !exists {
		return 0, nil
	}

	switch v := value.(type) {
	case int:
		return int64(v), nil
	case int64:
		return v, nil
	case float64:
		return int64(v), nil
	default:
		return 0, agentErrors.New(agentErrors.CodeTypeMismatch, "field is not an int").
			WithContext("field", key)
	}
}

// GetFloat 获取浮点数字段
func (ts *TypedState) GetFloat(key string) (float64, error) {
	value, exists := ts.Get(key)
	if !exists {
		return 0, nil
	}

	if f, ok := value.(float64); ok {
		return f, nil
	}

	return 0, agentErrors.New(agentErrors.CodeTypeMismatch, "field is not a float").
		WithContext("field", key)
}

// GetBool 获取布尔字段
func (ts *TypedState) GetBool(key string) (bool, error) {
	value, exists := ts.Get(key)
	if !exists {
		return false, nil
	}

	if b, ok := value.(bool); ok {
		return b, nil
	}

	return false, agentErrors.New(agentErrors.CodeTypeMismatch, "field is not a bool").
		WithContext("field", key)
}

// Set 设置字段值（带验证）
func (ts *TypedState) Set(key string, value interface{}) error {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	// Schema 验证
	if ts.schema != nil {
		field, defined := ts.schema.Fields[key]
		if !defined && ts.schema.StrictMode {
			return agentErrors.New(agentErrors.CodeStateValidation, "undefined field in strict mode").
				WithContext("field", key)
		}

		if defined {
			if err := ts.schema.validateField(field, value); err != nil {
				return err
			}
		}
	}

	ts.data[key] = value
	ts.dirty = true
	return nil
}

// SetBatch 批量设置字段值
func (ts *TypedState) SetBatch(values map[string]interface{}) error {
	// 先验证所有字段
	if ts.schema != nil {
		if err := ts.schema.Validate(values); err != nil {
			return err
		}
	}

	ts.mu.Lock()
	defer ts.mu.Unlock()

	for k, v := range values {
		ts.data[k] = v
	}
	ts.dirty = true
	return nil
}

// Delete 删除字段
func (ts *TypedState) Delete(key string) error {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	// 检查是否为必需字段
	if ts.schema != nil {
		if field, exists := ts.schema.Fields[key]; exists && field.Required {
			return agentErrors.New(agentErrors.CodeStateValidation, "cannot delete required field").
				WithContext("field", key)
		}
	}

	delete(ts.data, key)
	ts.dirty = true
	return nil
}

// Keys 返回所有键
func (ts *TypedState) Keys() []string {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	keys := make([]string, 0, len(ts.data))
	for k := range ts.data {
		keys = append(keys, k)
	}
	return keys
}

// ToMap 导出为普通 map（用于序列化）
func (ts *TypedState) ToMap() map[string]interface{} {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	result := make(map[string]interface{}, len(ts.data))
	for k, v := range ts.data {
		result[k] = v
	}
	return result
}

// FromMap 从 map 导入（带验证）
func (ts *TypedState) FromMap(data map[string]interface{}) error {
	if ts.schema != nil {
		if err := ts.schema.Validate(data); err != nil {
			return err
		}
	}

	ts.mu.Lock()
	defer ts.mu.Unlock()

	// 清空并重新填充
	ts.data = make(map[string]interface{}, len(data))
	for k, v := range data {
		ts.data[k] = v
	}
	ts.dirty = false
	return nil
}

// Clone 克隆状态
func (ts *TypedState) Clone() *TypedState {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	clone := &TypedState{
		schema: ts.schema,
		data:   make(map[string]interface{}, len(ts.data)),
		dirty:  false,
	}

	for k, v := range ts.data {
		clone.data[k] = v
	}

	return clone
}

// IsDirty 检查是否有未保存的更改
func (ts *TypedState) IsDirty() bool {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	return ts.dirty
}

// MarkClean 标记为已保存
func (ts *TypedState) MarkClean() {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.dirty = false
}

// Schema 返回关联的 Schema
func (ts *TypedState) Schema() *StateSchema {
	return ts.schema
}

// =============================================================================
// Serialization Support
// =============================================================================

// StateEnvelope 用于序列化的状态信封
//
// 包含元数据以支持版本兼容性和 Schema 验证
type StateEnvelope struct {
	// SchemaName Schema 名称
	SchemaName string `json:"schema_name,omitempty"`

	// SchemaVersion Schema 版本
	SchemaVersion string `json:"schema_version,omitempty"`

	// Data 实际状态数据
	Data map[string]interface{} `json:"data"`

	// Checksum 数据校验和（可选）
	Checksum string `json:"checksum,omitempty"`
}

// MarshalJSON 实现 json.Marshaler
func (ts *TypedState) MarshalJSON() ([]byte, error) {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	envelope := StateEnvelope{
		Data: ts.data,
	}

	if ts.schema != nil {
		envelope.SchemaName = ts.schema.Name
		envelope.SchemaVersion = ts.schema.Version
	}

	return json.Marshal(envelope)
}

// UnmarshalJSON 实现 json.Unmarshaler
func (ts *TypedState) UnmarshalJSON(data []byte) error {
	var envelope StateEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return agentErrors.Wrap(err, agentErrors.CodeStoreSerialization, "failed to unmarshal state").
			WithComponent("typed_state").
			WithOperation("unmarshal")
	}

	// Schema 版本检查
	if ts.schema != nil && envelope.SchemaVersion != "" {
		if envelope.SchemaVersion != ts.schema.Version {
			return agentErrors.New(agentErrors.CodeStateValidation, "schema version mismatch").
				WithComponent("typed_state").
				WithOperation("unmarshal").
				WithContext("expected_version", ts.schema.Version).
				WithContext("actual_version", envelope.SchemaVersion)
		}
	}

	return ts.FromMap(envelope.Data)
}

// =============================================================================
// Schema Registry
// =============================================================================

// SchemaRegistry 管理状态 Schema 定义
type SchemaRegistry struct {
	mu      sync.RWMutex
	schemas map[string]*StateSchema
}

// NewSchemaRegistry 创建 Schema 注册中心
func NewSchemaRegistry() *SchemaRegistry {
	return &SchemaRegistry{
		schemas: make(map[string]*StateSchema),
	}
}

// Register 注册 Schema
func (r *SchemaRegistry) Register(schema *StateSchema) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := fmt.Sprintf("%s:%s", schema.Name, schema.Version)
	if _, exists := r.schemas[key]; exists {
		return agentErrors.New(agentErrors.CodeAlreadyExists, "schema already registered").
			WithContext("name", schema.Name).
			WithContext("version", schema.Version)
	}

	r.schemas[key] = schema
	return nil
}

// Get 获取 Schema
func (r *SchemaRegistry) Get(name, version string) (*StateSchema, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	key := fmt.Sprintf("%s:%s", name, version)
	schema, exists := r.schemas[key]
	if !exists {
		return nil, agentErrors.New(agentErrors.CodeNotFound, "schema not found").
			WithContext("name", name).
			WithContext("version", version)
	}

	return schema, nil
}

// CreateState 使用注册的 Schema 创建状态
func (r *SchemaRegistry) CreateState(schemaName, version string) (*TypedState, error) {
	schema, err := r.Get(schemaName, version)
	if err != nil {
		return nil, err
	}
	return NewTypedState(schema), nil
}

// Global schema registry
var (
	globalSchemaRegistry     *SchemaRegistry
	globalSchemaRegistryOnce sync.Once
)

// GlobalSchemaRegistry 获取全局 Schema 注册中心
func GlobalSchemaRegistry() *SchemaRegistry {
	globalSchemaRegistryOnce.Do(func() {
		globalSchemaRegistry = NewSchemaRegistry()
	})
	return globalSchemaRegistry
}
