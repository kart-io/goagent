// Package state provides type-safe state management with schema validation.
//
// This file addresses the state serialization bottleneck problem:
//   - Replaces map[string]interface{} with typed, schema-validated state
//   - Provides contract enforcement for distributed systems
//   - Enables efficient serialization with known schemas
package state

import (
	"fmt"
	"math"
	"math/big"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/kart-io/goagent/codec/json"
	agentErrors "github.com/kart-io/goagent/errors"
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
	Default interface{} `json:"default,omitzero"`

	// ItemType 数组元素类型（仅当 Type == FieldTypeArray 时有效）
	ItemType FieldType `json:"item_type,omitempty"`

	// ObjectSchema 嵌套对象的 Schema（仅当 Type == FieldTypeObject 时有效）
	ObjectSchema *StateSchema `json:"object_schema,omitzero"`

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
	if !knownFieldType(field.Type) || (field.Type == FieldTypeArray && field.ItemType != "" && !knownFieldType(field.ItemType)) {
		return agentErrors.Newf(agentErrors.CodeStateValidation, "字段 %s 使用未知类型", field.Name)
	}
	if value == nil {
		if field.Required {
			return agentErrors.Newf(agentErrors.CodeStateValidation, "必需字段 %s 不能为空", field.Name)
		}
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
		objMap := make(map[string]interface{})
		iter := reflect.ValueOf(value).MapRange()
		for iter.Next() {
			objMap[iter.Key().String()] = iter.Value().Interface()
		}
		if err := field.ObjectSchema.Validate(objMap); err != nil {
			return err
		}
	}
	if field.Type == FieldTypeArray && field.ItemType != "" {
		items := reflect.ValueOf(value)
		for i := 0; i < items.Len(); i++ {
			if err := s.validateField(&FieldSchema{Name: fmt.Sprintf("%s[%d]", field.Name, i), Type: field.ItemType}, items.Index(i).Interface()); err != nil {
				return err
			}
		}
	}

	return nil
}

func knownFieldType(t FieldType) bool {
	switch t {
	case FieldTypeString, FieldTypeInt, FieldTypeFloat, FieldTypeBool, FieldTypeObject, FieldTypeArray, FieldTypeAny, FieldTypeBytes, FieldTypeTime:
		return true
	}
	return false
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
			return agentErrors.Newf(agentErrors.CodeStateValidation, "expected string, got %s", kind).WithComponent("state_schema").WithOperation("validate_type")
		}
	case FieldTypeInt:
		if _, err := integerValue(value); err != nil {
			return err
		}
	case FieldTypeFloat:
		if kind != reflect.Float64 && kind != reflect.Float32 {
			return agentErrors.Newf(agentErrors.CodeStateValidation, "expected float, got %s", kind).WithComponent("state_schema").WithOperation("validate_type")
		}
	case FieldTypeBool:
		if kind != reflect.Bool {
			return agentErrors.Newf(agentErrors.CodeStateValidation, "expected bool, got %s", kind).WithComponent("state_schema").WithOperation("validate_type")
		}
	case FieldTypeObject:
		if kind != reflect.Map || v.Type().Key().Kind() != reflect.String {
			return agentErrors.Newf(agentErrors.CodeStateValidation, "expected object, got %s", kind).WithComponent("state_schema").WithOperation("validate_type")
		}
	case FieldTypeArray:
		if kind != reflect.Slice && kind != reflect.Array {
			return agentErrors.Newf(agentErrors.CodeStateValidation, "expected array, got %s", kind).WithComponent("state_schema").WithOperation("validate_type")
		}
	case FieldTypeBytes:
		if kind != reflect.Slice || v.Type().Elem().Kind() != reflect.Uint8 {
			return agentErrors.Newf(agentErrors.CodeStateValidation, "期望字节切片，实际为 %T", value).WithComponent("state_schema").WithOperation("validate_type")
		}
	case FieldTypeTime:
		if _, ok := value.(time.Time); !ok {
			return agentErrors.Newf(agentErrors.CodeStateValidation, "期望time.Time，实际为 %T", value).WithComponent("state_schema").WithOperation("validate_type")
		}
	default:
		return agentErrors.Newf(agentErrors.CodeStateValidation, "未知字段类型 %q", expected).WithComponent("state_schema").WithOperation("validate_type")
	}

	return nil
}

// integerValue 将所有受支持整数表示约束到 int64 的有效范围，拒绝截断与溢出。
func integerValue(value interface{}) (int64, error) {
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int(), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if n := v.Uint(); n <= math.MaxInt64 {
			return int64(n), nil
		}
	case reflect.Float32, reflect.Float64:
		f := v.Float()
		if !math.IsNaN(f) && !math.IsInf(f, 0) && math.Trunc(f) == f && f >= -0x1p63 && f < 0x1p63 {
			return int64(f), nil
		}
	}
	return 0, agentErrors.Newf(agentErrors.CodeTypeMismatch, "期望int64范围内整数，实际为 %v", value)
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

	if v := reflect.ValueOf(value); v.Kind() == reflect.String {
		return v.String(), nil
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

	return integerValue(value)
}

// GetFloat 获取浮点数字段
func (ts *TypedState) GetFloat(key string) (float64, error) {
	value, exists := ts.Get(key)
	if !exists {
		return 0, nil
	}

	if v := reflect.ValueOf(value); v.Kind() == reflect.Float32 || v.Kind() == reflect.Float64 {
		return v.Float(), nil
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

	if v := reflect.ValueOf(value); v.Kind() == reflect.Bool {
		return v.Bool(), nil
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
		Data: encodeStateJSONFields(ts.schema, ts.data),
	}

	if ts.schema != nil {
		envelope.SchemaName = ts.schema.Name
		envelope.SchemaVersion = ts.schema.Version
	}

	return json.Marshal(envelope)
}

// encodeStateJSONFields 仅投影 schema 声明的容器形状，不修改宿主持有的值。
// Go 的字节切片默认编码为 base64；Array 必须显式展开，Bytes 则保留默认编码。
func encodeStateJSONFields(schema *StateSchema, data map[string]interface{}) map[string]interface{} {
	if schema == nil {
		return data
	}
	values := make(map[string]interface{}, len(data))
	for name, value := range data {
		values[name] = encodeStateJSONField(schema.Fields[name], value)
	}
	return values
}

func encodeStateJSONField(field *FieldSchema, value interface{}) interface{} {
	if field == nil || value == nil {
		return value
	}
	v := reflect.ValueOf(value)
	switch field.Type {
	case FieldTypeArray:
		if v.Kind() != reflect.Slice && v.Kind() != reflect.Array {
			return value
		}
		if v.Kind() == reflect.Slice && v.IsNil() {
			return []interface{}(nil)
		}
		items := make([]interface{}, v.Len())
		var item *FieldSchema
		if field.ItemType != "" {
			item = &FieldSchema{Type: field.ItemType}
		}
		for i := range items {
			items[i] = encodeStateJSONField(item, v.Index(i).Interface())
		}
		return items
	case FieldTypeObject:
		if field.ObjectSchema == nil || v.Kind() != reflect.Map || v.Type().Key().Kind() != reflect.String || v.IsNil() {
			return value
		}
		values := make(map[string]interface{}, v.Len())
		iter := v.MapRange()
		for iter.Next() {
			name := iter.Key().String()
			values[name] = encodeStateJSONField(field.ObjectSchema.Fields[name], iter.Value().Interface())
		}
		return values
	}
	return value
}

// UnmarshalJSON 实现 json.Unmarshaler
func (ts *TypedState) UnmarshalJSON(data []byte) error {
	// 先保留字段原文；整数不能先经过 float64，字节与时间只能在 JSON 边界恢复。
	var envelope struct {
		SchemaVersion string                     `json:"schema_version"`
		Data          map[string]json.RawMessage `json:"data"`
	}
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

	values, err := decodeStateJSONFields(ts.schema, envelope.Data)
	if err != nil {
		return agentErrors.Wrap(err, agentErrors.CodeStateValidation, "状态JSON字段恢复失败").WithComponent("typed_state").WithOperation("unmarshal")
	}
	// 全部恢复并验证成功后才替换现有状态，不留下部分修改。
	return ts.FromMap(values)
}

func decodeStateJSONFields(schema *StateSchema, raw map[string]json.RawMessage) (map[string]interface{}, error) {
	values := make(map[string]interface{}, len(raw))
	for name, data := range raw {
		var field *FieldSchema
		if schema != nil {
			field = schema.Fields[name]
		}
		value, err := decodeStateJSONField(field, data)
		if err != nil {
			return nil, fmt.Errorf("字段 %s: %w", name, err)
		}
		values[name] = value
	}
	return values, nil
}

func decodeStateJSONField(field *FieldSchema, data json.RawMessage) (interface{}, error) {
	if field != nil && strings.TrimSpace(string(data)) == "null" {
		switch field.Type {
		case FieldTypeBytes:
			return []byte(nil), nil
		case FieldTypeArray:
			return []interface{}(nil), nil
		case FieldTypeObject:
			if !field.Required {
				return nil, nil
			}
			return map[string]interface{}(nil), nil
		}
	}
	if field != nil && strings.TrimSpace(string(data)) != "null" {
		switch field.Type {
		case FieldTypeBytes:
			var value []byte
			err := json.Unmarshal(data, &value)
			return value, err
		case FieldTypeTime:
			var value time.Time
			err := json.Unmarshal(data, &value)
			return value, err
		case FieldTypeInt:
			var value interface{}
			if err := json.Unmarshal(data, &value, json.UseNumber); err != nil {
				return nil, err
			}
			if number, ok := value.(json.Number); ok {
				return decodeStateJSONInteger(number)
			}
			return value, nil
		case FieldTypeObject:
			if field.ObjectSchema != nil {
				var raw map[string]json.RawMessage
				if err := json.Unmarshal(data, &raw); err != nil {
					return nil, err
				}
				return decodeStateJSONFields(field.ObjectSchema, raw)
			}
		case FieldTypeArray:
			if field.ItemType != "" {
				var raw []json.RawMessage
				if err := json.Unmarshal(data, &raw); err != nil {
					return nil, err
				}
				values := make([]interface{}, len(raw))
				for i, item := range raw {
					value, err := decodeStateJSONField(&FieldSchema{Type: field.ItemType}, item)
					if err != nil {
						return nil, fmt.Errorf("元素 %d: %w", i, err)
					}
					values[i] = value
				}
				return values, nil
			}
		}
	}
	var value interface{}
	err := json.Unmarshal(data, &value)
	return value, err
}

// decodeStateJSONInteger 只在 JSON 边界将精确整数原文转为 int64；普通 FromMap 不接受数字字符串。
func decodeStateJSONInteger(number json.Number) (int64, error) {
	f, err := number.Float64()
	if err != nil {
		return 0, agentErrors.Wrapf(err, agentErrors.CodeStateValidation, "JSON整数超出int64范围: %s", number).WithComponent("typed_state").WithOperation("decode_integer")
	}
	if f < -0x1p63 || f > 0x1p63 {
		return 0, agentErrors.Newf(agentErrors.CodeStateValidation, "JSON整数超出int64范围: %s", number).WithComponent("typed_state").WithOperation("decode_integer")
	}
	if f == 0 {
		// 巨大负指数无需构建任意精度分母；非零尾数下溢仍然是小数。
		mantissa := strings.FieldsFunc(string(number), func(r rune) bool { return r == 'e' || r == 'E' })[0]
		if strings.ContainsAny(mantissa, "123456789") {
			return 0, agentErrors.Newf(agentErrors.CodeStateValidation, "JSON数值不是整数: %s", number).WithComponent("typed_state").WithOperation("decode_integer")
		}
		return 0, nil
	}
	rational, ok := new(big.Rat).SetString(string(number))
	if !ok || !rational.IsInt() || !rational.Num().IsInt64() {
		return 0, agentErrors.Newf(agentErrors.CodeStateValidation, "JSON数值不是int64范围内整数: %s", number).WithComponent("typed_state").WithOperation("decode_integer")
	}
	return rational.Num().Int64(), nil
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
