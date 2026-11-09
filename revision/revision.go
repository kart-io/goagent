// Package revision 提供有界、内容寻址的 Agent 定义与资源归档。
package revision

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/skills"
)

// Reference 固定外部资源的标识与版本，由宿主负责解析。
type Reference struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}

// Runtime 固定运行时种类、版本和精确摘要，不解析动态 latest。
// Artifact 引用归档二进制；Image 只固定 OCI 引用，镜像层由宿主保留与拉取。
type Runtime struct {
	Kind     string `json:"kind"`
	Version  string `json:"version"`
	Digest   string `json:"digest"`
	Artifact string `json:"artifact,omitempty"`
	Image    string `json:"image,omitempty"`
}

// Definition 包含完整定义；Skills 将技能名称映射到 Files 下的相对目录。
type Definition struct {
	Config  json.RawMessage      `json:"config"`
	Prompts map[string]string    `json:"prompts"`
	Tools   json.RawMessage      `json:"tools"`
	Models  map[string]Reference `json:"models"`
	Policy  Reference            `json:"policy"`
	Skills  map[string]string    `json:"skills"`
	Runtime Runtime              `json:"runtime"`
}

// AgentRevision 的身份只覆盖定义和文件字节，不包含归档时间。
type AgentRevision struct {
	ID         string            `json:"id"`
	Definition Definition        `json:"definition"`
	Files      map[string][]byte `json:"files"`
}

// Archive 按精确摘要读取修订，不提供动态版本选择。
type Archive interface {
	Load(context.Context, string) (AgentRevision, error)
}

// Seal 规范化 JSON 并深复制全部输入。Config 必须为对象，nil Tools 规范为 []。
// 数字保留原文，因此不同数字写法不会被浮点转换合并。
func Seal(def Definition, files map[string][]byte) (AgentRevision, error) {
	if err := boundDefinition(def); err != nil {
		return AgentRevision{}, err
	}
	if !validText(def.Policy.ID) || !validText(def.Policy.Version) {
		return AgentRevision{}, invalid("策略必须具有标识和版本")
	}
	for name, ref := range def.Models {
		if !validText(name) || !validText(ref.ID) || !validText(ref.Version) {
			return AgentRevision{}, invalid("模型必须具有名称、标识和版本")
		}
	}
	var err error
	def.Config, err = canonicalJSON(def.Config)
	if err != nil {
		return AgentRevision{}, err
	}
	if len(def.Config) == 0 || def.Config[0] != '{' {
		return AgentRevision{}, invalid("配置必须是 JSON 对象")
	}
	if def.Tools == nil {
		def.Tools = json.RawMessage("[]")
	}
	def.Tools, err = canonicalJSON(def.Tools)
	if err != nil {
		return AgentRevision{}, err
	}
	def.Prompts = cloneMap(def.Prompts)
	def.Models = cloneMap(def.Models)
	def.Skills = cloneMap(def.Skills)
	encodedDef, err := json.Marshal(def)
	if err != nil {
		return AgentRevision{}, invalid("定义编码失败：%v", err)
	}
	if len(encodedDef) > constants.MaxRevisionDefinitionBytes {
		return AgentRevision{}, invalid("定义超过字节上限")
	}
	if err := validateFiles(files, len(encodedDef)); err != nil {
		return AgentRevision{}, err
	}
	if err := validateRuntime(def.Runtime, files); err != nil {
		return AgentRevision{}, err
	}
	for name, dir := range def.Skills {
		if !validPath(dir, true) {
			return AgentRevision{}, invalid("技能目录无效：%q", dir)
		}
		doc, ok := files[path.Join(dir, "SKILL.md")]
		if !ok {
			return AgentRevision{}, invalid("技能 %q 缺少 SKILL.md", name)
		}
		skill, err := skills.Parse(doc)
		if err != nil {
			return AgentRevision{}, invalid("技能 %q 无效：%v", name, err)
		}
		if skill.Metadata.Name != name {
			return AgentRevision{}, invalid("技能名称不匹配：%q", name)
		}
	}
	copyFiles := make(map[string][]byte, len(files))
	for name, data := range files {
		copyFiles[name] = append([]byte{}, data...)
	}
	out := AgentRevision{Definition: def, Files: copyFiles}
	content, err := json.Marshal(struct {
		Definition Definition        `json:"definition"`
		Files      map[string][]byte `json:"files"`
	}{def, copyFiles})
	if err != nil {
		return AgentRevision{}, invalid("修订编码失败：%v", err)
	}
	out.ID = fmt.Sprintf("%x", sha256.Sum256(content))
	return out, nil
}

// Verify 校验全部约束并重算规范内容摘要；不修改传入值。
func Verify(rev AgentRevision) error {
	if !validDigest(rev.ID) {
		return invalid("修订标识必须是小写 SHA-256 摘要")
	}
	sealed, err := Seal(rev.Definition, rev.Files)
	if err != nil {
		return err
	}
	if sealed.ID != rev.ID {
		return invalid("修订摘要与内容不符")
	}
	return nil
}

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w：%s", fs.ErrInvalid, fmt.Sprintf(format, args...))
}

func validText(s string) bool { return s != "" && strings.TrimSpace(s) == s && utf8.ValidString(s) }

func validDigest(s string) bool {
	if len(s) != sha256.Size*2 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func validateRuntime(runtime Runtime, files map[string][]byte) error {
	if !validText(runtime.Version) || !validDigest(runtime.Digest) {
		return invalid("运行时必须具有版本和小写 SHA-256 摘要")
	}
	switch runtime.Kind {
	case "native":
		if runtime.Artifact != "" || runtime.Image != "" {
			return invalid("原生运行时不得指定归档程序或镜像")
		}
	case "executable":
		if !validPath(runtime.Artifact, false) || runtime.Image != "" {
			return invalid("程序运行时必须指定归档文件且不得指定镜像")
		}
		data, ok := files[runtime.Artifact]
		if !ok || fmt.Sprintf("%x", sha256.Sum256(data)) != runtime.Digest {
			return invalid("归档程序不存在或摘要不匹配")
		}
	case "oci":
		if !validText(runtime.Image) || runtime.Artifact != "" || !strings.HasSuffix(runtime.Image, "@sha256:"+runtime.Digest) {
			return invalid("OCI 运行时必须指定匹配摘要的镜像且不得指定归档程序")
		}
	default:
		return invalid("运行时种类必须为 native、executable 或 oci")
	}
	return nil
}

func validPath(name string, root bool) bool {
	return len(name) <= constants.MaxRevisionPathBytes && (root || name != ".") && fs.ValidPath(name) && !strings.ContainsAny(name, "\\\x00:")
}

func cloneMap[V any](src map[string]V) map[string]V {
	dst := make(map[string]V, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

// 先计算原始长度，避免巨大输入在编码或复制阶段产生额外分配。
func boundDefinition(def Definition) error {
	n := 0
	add := func(s string) bool {
		if len(s) > constants.MaxRevisionDefinitionBytes-n || !utf8.ValidString(s) {
			return false
		}
		n += len(s)
		return true
	}
	if len(def.Config) > constants.MaxRevisionDefinitionBytes || len(def.Tools) > constants.MaxRevisionDefinitionBytes-len(def.Config) {
		return invalid("定义超过字节上限")
	}
	n = len(def.Config) + len(def.Tools)
	for _, s := range []string{def.Policy.ID, def.Policy.Version, def.Runtime.Kind, def.Runtime.Version, def.Runtime.Digest, def.Runtime.Artifact, def.Runtime.Image} {
		if !add(s) {
			return invalid("定义超过字节上限或包含非法 UTF-8")
		}
	}
	for k, v := range def.Models {
		if !add(k) || !add(v.ID) || !add(v.Version) || !add("{}") {
			return invalid("模型定义超过上限或包含非法 UTF-8")
		}
	}
	for _, m := range []map[string]string{def.Prompts, def.Skills} {
		for k, v := range m {
			if !add(k) || !add(v) || !add("{}") {
				return invalid("定义超过上限或包含非法 UTF-8")
			}
		}
	}
	return nil
}

func validateFiles(files map[string][]byte, definitionSize int) error {
	if len(files) > constants.MaxRevisionFiles {
		return invalid("文件数量超过上限")
	}
	// 外层字段与 64 字符摘要的固定余量，编码路径另行精确计数。
	size := definitionSize + len(`{"id":"","definition":,"files":{}}`) + sha256.Size*2
	for name, data := range files {
		if len(name) > constants.MaxRevisionPathBytes {
			return invalid("文件路径超过字节上限")
		}
		if !validPath(name, false) {
			return invalid("文件路径无效：%q", name)
		}
		if len(data) > constants.MaxRevisionFileBytes {
			return invalid("文件 %q 超过字节上限", name)
		}
		key, err := json.Marshal(name)
		if err != nil {
			return invalid("文件路径编码失败：%v", err)
		}
		size += len(key) + 4 + ((len(data)+2)/3)*4
		if size > constants.MaxRevisionArchiveBytes {
			return invalid("归档超过字节上限")
		}
		for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
			if _, ok := files[parent]; ok {
				return invalid("文件与祖先路径冲突：%q", name)
			}
		}
	}
	return nil
}

func canonicalJSON(raw []byte) ([]byte, error) {
	if err := checkJSONDepth(raw); err != nil {
		return nil, err
	}
	var value any
	if err := json.Unmarshal(raw, &value, json.Strict, json.UseNumber); err != nil {
		return nil, invalid("JSON 无效：%v", err)
	}
	return json.Marshal(value)
}

// 先流式检查深度，再创建树状值，避免嵌套输入耗尽栈。
func checkJSONDepth(raw []byte) error {
	dec := json.NewDecoder(bytes.NewReader(raw), json.Strict)
	for {
		_, err := dec.ReadToken()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return invalid("JSON 无效：%v", err)
		}
		if dec.StackDepth() > constants.MaxRevisionJSONDepth {
			return invalid("JSON 嵌套超过上限")
		}
	}
}
