// Package codec 是 goagent 统一的数据格式转换入口，结构与 Kubernetes 一致：每种格式一个子包
// （codec/json、codec/yaml）提供包级 Marshal、Unmarshal 等函数，本包提供按格式名或扩展名选择的
// Codec 接口、ReadFile 与按 json 标签的 Convert。除 codec 及其子包外，代码不直接导入 JSON、YAML
// 等格式库（由 apicheck 检查）。
//
// 结构体只需 json 标签：YAML 解码先转为 JSON 再按 json 标签解码，编码先得到 JSON 再转为 YAML，
// 两种格式共用自定义 JSON 方法、时长表示与字段规则。框架的持久化（意图指纹、幂等键、持久记录）
// 固定使用 JSON，不提供切换默认格式的开关，否则指纹变化会使在途运行无法恢复。
package codec

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/codec/yaml"
)

// Codec 是一种数据格式的编解码器，用于格式在运行时才确定的场景；格式确定时直接调用子包函数。
// opts 作用于 JSON 阶段（YAML 也经过 JSON），在默认规则之后生效，可用 json.Strict、json.UseNumber 等覆盖。
type Codec interface {
	// Name 返回格式名，如 "json"、"yaml"。
	Name() string
	Marshal(v any, opts ...json.Option) ([]byte, error)
	// Unmarshal 把 data 解码到 v，v 须为非 nil 指针；解码合并到已有值。
	Unmarshal(data []byte, v any, opts ...json.Option) error
}

var (
	jsonCodec Codec = &format{"json", json.Marshal, json.Unmarshal}
	yamlCodec Codec = &format{"yaml", yaml.Marshal, yaml.Unmarshal}

	byName = map[string]Codec{"json": jsonCodec, "yaml": yamlCodec, "yml": yamlCodec}
	byExt  = map[string]Codec{".json": jsonCodec, ".yaml": yamlCodec, ".yml": yamlCodec}
)

type format struct {
	name      string
	marshal   func(any, ...json.Option) ([]byte, error)
	unmarshal func([]byte, any, ...json.Option) error
}

func (f *format) Name() string { return f.name }
func (f *format) Marshal(v any, opts ...json.Option) ([]byte, error) {
	return f.marshal(v, opts...)
}
func (f *format) Unmarshal(data []byte, v any, opts ...json.Option) error {
	return f.unmarshal(data, v, opts...)
}

// ByName 按格式名（json、yaml、yml，不区分大小写）查找编解码器。
func ByName(name string) (Codec, bool) {
	c, ok := byName[strings.ToLower(name)]
	return c, ok
}

// ByExt 按文件扩展名（.json、.yaml、.yml，不区分大小写）查找编解码器。
func ByExt(path string) (Codec, bool) {
	c, ok := byExt[strings.ToLower(filepath.Ext(path))]
	return c, ok
}

// ReadFile 按扩展名选择编解码器，把文件解码到 v；配置文件通常传 json.Strict。
// 空文件视为未配置，v 保持原值，可先填默认值再加载。
func ReadFile(path string, v any, opts ...json.Option) error {
	c, ok := ByExt(path)
	if !ok {
		return fmt.Errorf("文件 %s：不支持的扩展名 %q，仅支持 .json、.yaml、.yml", path, filepath.Ext(path))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("读取文件: %w", err)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	if err := c.Unmarshal(data, v, opts...); err != nil {
		return fmt.Errorf("文件 %s: %w", path, err)
	}
	return nil
}
