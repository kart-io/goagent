// Package skills 提供与 Agent 执行和装配无关的只读本地技能目录。
// 调用方决定来源、顺序、授权与提示词；本包不执行技能中的脚本或工具声明。
package skills

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"maps"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/kart-io/goagent/codec/yaml"
	"github.com/kart-io/goagent/constants"
	agenterrors "github.com/kart-io/goagent/errors"
)

// Metadata 是技能目录项。Attributes 对应 frontmatter 的 metadata；Version
// 来自 metadata.version，仅为作者声明。AllowedTools 和 DisableModelInvocation
// 是提供给宿主的信息，不会授予权限或自动过滤 Catalog。
type Metadata struct {
	Name                   string            `json:"name"`
	Description            string            `json:"description"`
	License                string            `json:"license,omitempty"`
	Compatibility          string            `json:"compatibility,omitempty"`
	Attributes             map[string]string `json:"metadata,omitempty"`
	AllowedTools           string            `json:"allowed-tools,omitempty"`
	DisableModelInvocation bool              `json:"disable-model-invocation,omitempty"`
	// Source 是发现时 SKILL.md 的路径，仅用于追踪；读取始终使用持有的目录句柄。
	Source  string `json:"source,omitempty"`
	Version string `json:"version,omitempty"`
	// Digest 是原始 SKILL.md 全文的 SHA-256 十六进制摘要，不包含附属文件。
	Digest string `json:"digest"`
}

// Skill 是一份技能说明。Parse 不扫描文件，Files 仅由 Catalog.Load 填充，
// 文件名使用相对技能目录的斜线分隔路径，按字典序排列。
type Skill struct {
	Metadata Metadata `json:"metadata"`
	Body     string   `json:"body"`
	Files    []string `json:"files"`
}

var namePattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// Parse 解析带 YAML frontmatter 的 UTF-8 SKILL.md，支持 BOM、LF 与 CRLF。
// 名称、描述和兼容性说明按 Agent Skills 格式校验；未知扩展字段被忽略。
// 正文去除首尾空白，摘要仍覆盖输入原文。目录名匹配由 Discover 校验。
func Parse(data []byte) (Skill, error) {
	if len(data) > constants.SkillMaxDocumentBytes {
		return Skill{}, agenterrors.New(agenterrors.CodeInvalidInput, "SKILL.md exceeds document byte limit")
	}
	if !utf8.Valid(data) {
		return Skill{}, agenterrors.New(agenterrors.CodeInvalidInput, "SKILL.md must be UTF-8")
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(data))
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
	first, rest, ok := bytes.Cut(data, []byte("\n"))
	if !ok || !delimiter(first) {
		return Skill{}, agenterrors.New(agenterrors.CodeInvalidInput, "缺少 YAML frontmatter")
	}
	end, body := -1, []byte(nil)
	for offset := 0; offset < len(rest); {
		line, tail, newline := bytes.Cut(rest[offset:], []byte("\n"))
		if delimiter(line) {
			end, body = offset, tail
			break
		}
		if !newline {
			break
		}
		offset += len(line) + 1
	}
	if end < 0 {
		return Skill{}, agenterrors.New(agenterrors.CodeInvalidInput, "frontmatter 未结束")
	}
	fields, err := yaml.DecodeMapping(rest[:end], constants.SkillMaxDocumentBytes)
	if err != nil {
		return Skill{}, agenterrors.Wrap(err, agenterrors.CodeInvalidInput, "invalid skill frontmatter")
	}
	front, err := parseMetadata(fields)
	if err != nil {
		return Skill{}, err
	}
	if len(front.Name) > 64 || !namePattern.MatchString(front.Name) {
		return Skill{}, agenterrors.Newf(agenterrors.CodeInvalidInput, "name %q 须为 1–64 个小写字母、数字与单连字符", front.Name)
	}
	description := strings.TrimSpace(front.Description)
	if n := utf8.RuneCountInString(description); n == 0 || n > 1024 {
		return Skill{}, agenterrors.New(agenterrors.CodeInvalidInput, "description 须为 1–1024 个字符")
	}
	if _, exists := fields["compatibility"]; exists {
		front.Compatibility = strings.TrimSpace(front.Compatibility)
		if n := utf8.RuneCountInString(front.Compatibility); n == 0 || n > 500 {
			return Skill{}, agenterrors.New(agenterrors.CodeInvalidInput, "compatibility 须为 1–500 个字符")
		}
	}
	front.Description, front.Digest = description, digest
	return Skill{
		Metadata: front, Body: strings.TrimSpace(string(body)),
	}, nil
}

func parseMetadata(fields map[string]any) (Metadata, error) {
	var m Metadata
	for _, field := range []struct {
		name string
		dest *string
	}{
		{"name", &m.Name}, {"description", &m.Description}, {"license", &m.License},
		{"compatibility", &m.Compatibility}, {"allowed-tools", &m.AllowedTools},
	} {
		if value, exists := fields[field.name]; exists {
			text, ok := value.(string)
			if !ok {
				return Metadata{}, agenterrors.Newf(agenterrors.CodeInvalidInput, "%s must be a string", field.name)
			}
			*field.dest = text
		}
	}
	if value, exists := fields["disable-model-invocation"]; exists {
		flag, ok := value.(bool)
		if !ok {
			return Metadata{}, agenterrors.New(agenterrors.CodeInvalidInput, "disable-model-invocation must be a boolean")
		}
		m.DisableModelInvocation = flag
	}
	if value, exists := fields["metadata"]; exists {
		attributes, ok := value.(map[string]any)
		if !ok {
			return Metadata{}, agenterrors.New(agenterrors.CodeInvalidInput, "metadata must be a string-to-string mapping")
		}
		m.Attributes = make(map[string]string, len(attributes))
		for key, value := range attributes {
			text, ok := value.(string)
			if !ok {
				return Metadata{}, agenterrors.Newf(agenterrors.CodeInvalidInput, "metadata.%s must be a string", key)
			}
			m.Attributes[key] = text
		}
	}
	m.Version = m.Attributes["version"]
	return m, nil
}

func delimiter(line []byte) bool {
	return string(bytes.TrimRight(line, "\r\t ")) == "---"
}

func cloneMetadata(m Metadata) Metadata {
	m.Attributes = maps.Clone(m.Attributes)
	return m
}
