package revision

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/constants"
)

func revisionFixture() (Definition, map[string][]byte) {
	return Definition{
		Config:  json.RawMessage(`{"name":"归档代理","nested":{"b":2,"a":1}}`),
		Prompts: map[string]string{"system": "请读取归档内的运行手册"},
		Tools:   json.RawMessage(`[{"name":"read","parameters":{"b":2,"a":1}}]`),
		Models:  map[string]Reference{"primary": {ID: "model-a", Version: "v1"}},
		Policy:  Reference{ID: "policy-a", Version: "v1"},
		Skills:  map[string]string{"oncall": "skills/oncall"},
		Runtime: Runtime{Kind: "native", Version: "v1", Digest: strings.Repeat("a", 64)},
	}, map[string][]byte{
		"skills/oncall/SKILL.md":             []byte("---\nname: oncall\ndescription: 排查服务故障\n---\n请读取 resources/runbook.md。\n"),
		"skills/oncall/resources/runbook.md": []byte("保留这份运行手册的旧内容"),
		"skills/oncall/templates/report.txt": []byte("报告：{{.Summary}}"),
		"skills/oncall/programs/check.bin":   {0, 1, 255, 10},
	}
}

func sealFixture(t *testing.T) AgentRevision {
	t.Helper()
	def, files := revisionFixture()
	r, err := Seal(def, files)
	require.NoError(t, err, "有效定义应可封存")
	return r
}

func TestSealCanonicalIdentity(t *testing.T) {
	a, files := revisionFixture()
	a.Config = json.RawMessage(`{"z":[{"b":2,"a":1}],"large":9007199254740993,"a":true}`)
	b, otherFiles := revisionFixture()
	b.Config = json.RawMessage(" { \"a\": true, \"large\":9007199254740993, \"z\":[{\"a\":1,\"b\":2}] } ")
	b.Tools = json.RawMessage(` [ {"parameters":{"a":1,"b":2},"name":"read"} ] `)
	r1, err := Seal(a, files)
	require.NoError(t, err, "首份定义应可封存")
	r2, err := Seal(b, otherFiles)
	require.NoError(t, err, "键顺序不同的定义应可封存")
	require.Equal(t, r1.ID, r2.ID, "对象键顺序和空白不应改变内容标识")
	require.Equal(t, r1, r2, "封存结果应采用相同规范形式")
	require.Regexp(t, `^[0-9a-f]{64}$`, r1.ID, "内容标识须为小写 SHA-256")
	require.Contains(t, string(r1.Definition.Config), "9007199254740993", "不得通过浮点数损失大整数精度")
	require.NoError(t, Verify(r1), "刚封存的修订应通过验证")
	b.Config = json.RawMessage(`{"a":true,"large":9007199254740992,"z":[{"a":1,"b":2}]}`)
	r3, err := Seal(b, otherFiles)
	require.NoError(t, err)
	require.NotEqual(t, r1.ID, r3.ID, "相邻大整数必须产生不同标识")
}

func TestSealNormalizesNilToolsAndAllowsNoModels(t *testing.T) {
	def, files := revisionFixture()
	def.Tools, def.Models = nil, nil
	r, err := Seal(def, files)
	require.NoError(t, err, "无模型运行时允许空模型映射")
	require.JSONEq(t, `[]`, string(r.Definition.Tools), "缺省工具应规范为数组")
	def.Tools = json.RawMessage(`[]`)
	explicit, err := Seal(def, files)
	require.NoError(t, err)
	require.Equal(t, r.ID, explicit.ID, "缺省工具与显式空数组应有相同标识")
}

func runtimeFixture(kind string) (Definition, map[string][]byte) {
	def, files := revisionFixture()
	def.Runtime.Kind = kind
	switch kind {
	case "executable":
		def.Runtime.Artifact = "skills/oncall/programs/check.bin"
		def.Runtime.Digest = fmt.Sprintf("%x", sha256.Sum256(files[def.Runtime.Artifact]))
	case "oci":
		def.Runtime.Image = "registry.example/agent@sha256:" + def.Runtime.Digest
	}
	return def, files
}

func TestSealRuntimeVariants(t *testing.T) {
	for _, kind := range []string{"native", "executable", "oci"} {
		t.Run(kind, func(t *testing.T) {
			def, files := runtimeFixture(kind)
			r, err := Seal(def, files)
			require.NoError(t, err, "有效运行时应可封存")
			require.Equal(t, def.Runtime, r.Definition.Runtime, "运行时定位字段必须完整保留")
			require.NoError(t, Verify(r))
			encoded, err := json.Marshal(r.Definition.Runtime)
			require.NoError(t, err)
			var fields map[string]string
			require.NoError(t, json.Unmarshal(encoded, &fields))
			if kind == "executable" {
				require.Equal(t, def.Runtime.Artifact, fields["artifact"], "二进制路径须使用约定字段名")
			} else {
				require.NotContains(t, fields, "artifact", "空二进制路径必须省略")
			}
			if kind == "oci" {
				require.Equal(t, def.Runtime.Image, fields["image"], "镜像引用须使用约定字段名")
			} else {
				require.NotContains(t, fields, "image", "空镜像引用必须省略")
			}
			def.Runtime.Version = ""
			_, err = Seal(def, files)
			require.ErrorIs(t, err, fs.ErrInvalid, "每一种运行时均必须固定版本")
		})
	}
}

func TestSealRejectsInvalidRuntimeVariants(t *testing.T) {
	for _, tc := range []struct {
		name   string
		kind   string
		mutate func(*Runtime, map[string][]byte)
	}{
		{"未知种类", "host-specific-runtime", func(_ *Runtime, _ map[string][]byte) {}},
		{"原生携带二进制", "native", func(r *Runtime, _ map[string][]byte) { r.Artifact = "skills/oncall/programs/check.bin" }},
		{"原生携带镜像", "native", func(r *Runtime, _ map[string][]byte) { r.Image = "registry.example/agent@sha256:" + r.Digest }},
		{"可执行缺少路径", "executable", func(r *Runtime, _ map[string][]byte) { r.Artifact = "" }},
		{"可执行文件不存在", "executable", func(r *Runtime, f map[string][]byte) { delete(f, r.Artifact) }},
		{"可执行引用目录", "executable", func(r *Runtime, _ map[string][]byte) { r.Artifact = "skills/oncall/programs" }},
		{"可执行路径穿越", "executable", func(r *Runtime, _ map[string][]byte) { r.Artifact = "../check.bin" }},
		{"可执行绝对路径", "executable", func(r *Runtime, _ map[string][]byte) { r.Artifact = "/check.bin" }},
		{"可执行路径别名", "executable", func(r *Runtime, _ map[string][]byte) { r.Artifact = "skills/oncall/programs/./check.bin" }},
		{"可执行摘要不匹配", "executable", func(r *Runtime, _ map[string][]byte) { r.Digest = strings.Repeat("b", 64) }},
		{"可执行字节已修改", "executable", func(r *Runtime, f map[string][]byte) { f[r.Artifact][0]++ }},
		{"可执行携带镜像", "executable", func(r *Runtime, _ map[string][]byte) { r.Image = "registry.example/agent@sha256:" + r.Digest }},
		{"镜像引用为空", "oci", func(r *Runtime, _ map[string][]byte) { r.Image = "" }},
		{"镜像只有标签", "oci", func(r *Runtime, _ map[string][]byte) { r.Image = "registry.example/agent:latest" }},
		{"镜像摘要不匹配", "oci", func(r *Runtime, _ map[string][]byte) {
			r.Image = "registry.example/agent@sha256:" + strings.Repeat("b", 64)
		}},
		{"镜像摘要大写", "oci", func(r *Runtime, _ map[string][]byte) {
			r.Image = "registry.example/agent@sha256:" + strings.ToUpper(r.Digest)
		}},
		{"镜像摘要后缀多余", "oci", func(r *Runtime, _ map[string][]byte) { r.Image += ":latest" }},
		{"镜像携带二进制", "oci", func(r *Runtime, _ map[string][]byte) { r.Artifact = "skills/oncall/programs/check.bin" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			def, files := runtimeFixture(tc.kind)
			tc.mutate(&def.Runtime, files)
			_, err := Seal(def, files)
			require.ErrorIs(t, err, fs.ErrInvalid, "运行时字段组合或摘要不符合契约时必须拒绝")
		})
	}
}

func TestSealPreservesExactNumbers(t *testing.T) {
	// 这些值不能无损转换为 float64；规范化只能调整键序与空白。
	for _, number := range []string{"0.123456789012345678901234567890123456789", "1e9999999999999999999999999999999999999999", "-123456789012345678901234567890123456789"} {
		t.Run(number, func(t *testing.T) {
			def, files := revisionFixture()
			def.Config = json.RawMessage(`{"number":` + number + `}`)
			def.Tools = json.RawMessage(`[{"number":` + number + `}]`)
			r, err := Seal(def, files)
			require.NoError(t, err, "合法 JSON 数字不得受机器浮点范围限制")
			require.Equal(t, string(def.Config), string(r.Definition.Config), "配置数字原文应完整保留")
			require.Equal(t, string(def.Tools), string(r.Definition.Tools), "工具数字原文应完整保留")
			require.NoError(t, Verify(r))
		})
	}
}

func TestSealAcceptsRootSkillDirectory(t *testing.T) {
	def, files := revisionFixture()
	def.Skills["oncall"] = "."
	files["SKILL.md"] = files["skills/oncall/SKILL.md"]
	delete(files, "skills/oncall/SKILL.md")
	r, err := Seal(def, files)
	require.NoError(t, err, "技能允许直接位于归档根目录")
	require.Equal(t, ".", r.Definition.Skills["oncall"])
	require.NoError(t, Verify(r))
}

func TestSealDeepCopyIsolation(t *testing.T) {
	def, files := revisionFixture()
	r, err := Seal(def, files)
	require.NoError(t, err)
	want := sealFixture(t)
	def.Config[0], def.Tools[0] = '!', '!'
	def.Prompts["system"] = "变更后的提示词"
	def.Models["primary"] = Reference{ID: "new-model", Version: "v2"}
	def.Skills["oncall"] = "elsewhere"
	files["skills/oncall/programs/check.bin"][0] = 99
	delete(files, "skills/oncall/SKILL.md")
	require.Equal(t, want, r, "调用方修改原始切片和映射不得影响封存结果")
	require.NoError(t, Verify(r))

	def, files = revisionFixture()
	r, err = Seal(def, files)
	require.NoError(t, err)
	r.Definition.Config[0], r.Definition.Tools[0] = '!', '!'
	r.Definition.Prompts["system"] = "返回值已变更"
	r.Definition.Models["primary"] = Reference{}
	r.Definition.Skills["oncall"] = "elsewhere"
	r.Files["skills/oncall/programs/check.bin"][0] = 42
	expectedDef, expectedFiles := revisionFixture()
	require.Equal(t, expectedDef, def, "修改返回值不得反向污染输入定义")
	require.Equal(t, expectedFiles, files, "修改返回文件不得反向污染输入字节")
}

func TestSealRejectsInvalidJSON(t *testing.T) {
	for name, config := range map[string]string{
		"空配置": "", "空值": "null", "数组": "[]", "字符串": `"配置"`,
		"重复键": `{"x":1,"x":2}`, "嵌套重复键": `{"x":{"a":1,"a":2}}`,
		"转义后重复键": `{"a":1,"\u0061":2}`, "尾随值": `{} {}`,
		"截断对象": `{"a":`, "非法编码": "{\"x\":\"\xff\"}",
	} {
		t.Run(name, func(t *testing.T) {
			def, files := revisionFixture()
			def.Config = json.RawMessage(config)
			_, err := Seal(def, files)
			require.ErrorIs(t, err, fs.ErrInvalid, "无效配置应返回标准无效输入错误")
		})
	}
	for name, tools := range map[string]string{
		"工具重复键":   `[{"name":"a","name":"b"}]`,
		"工具嵌套重复键": `[{"parameters":{"x":1,"x":2}}]`,
		"工具尾随值":   `[] []`, "工具截断": `[`,
	} {
		t.Run(name, func(t *testing.T) {
			def, files := revisionFixture()
			def.Tools = json.RawMessage(tools)
			_, err := Seal(def, files)
			require.ErrorIs(t, err, fs.ErrInvalid, "无效工具 JSON 应被拒绝")
		})
	}
	def, files := revisionFixture()
	def.Config = nil
	_, err := Seal(def, files)
	require.ErrorIs(t, err, fs.ErrInvalid, "缺省配置必须被拒绝")
}

func TestSealRejectsInvalidReferencesAndRuntime(t *testing.T) {
	for name, mutate := range map[string]func(*Definition){
		"缺少策略标识":  func(d *Definition) { d.Policy.ID = "" },
		"缺少策略版本":  func(d *Definition) { d.Policy.Version = "" },
		"缺少模型标识":  func(d *Definition) { d.Models["primary"] = Reference{Version: "v1"} },
		"缺少模型版本":  func(d *Definition) { d.Models["primary"] = Reference{ID: "model-a"} },
		"缺少运行时种类": func(d *Definition) { d.Runtime.Kind = "" },
		"缺少运行时版本": func(d *Definition) { d.Runtime.Version = "" },
		"缺少运行时摘要": func(d *Definition) { d.Runtime.Digest = "" },
		"摘要过短":    func(d *Definition) { d.Runtime.Digest = strings.Repeat("a", 63) },
		"摘要过长":    func(d *Definition) { d.Runtime.Digest = strings.Repeat("a", 65) },
		"摘要大写":    func(d *Definition) { d.Runtime.Digest = strings.Repeat("A", 64) },
		"摘要非十六进制": func(d *Definition) { d.Runtime.Digest = strings.Repeat("g", 64) },
		"摘要带前缀":   func(d *Definition) { d.Runtime.Digest = "sha256:" + strings.Repeat("a", 64) },
	} {
		t.Run(name, func(t *testing.T) {
			def, files := revisionFixture()
			mutate(&def)
			_, err := Seal(def, files)
			require.ErrorIs(t, err, fs.ErrInvalid, "无效引用或运行时必须被拒绝")
		})
	}
}

func TestSealRejectsUnsafeFiles(t *testing.T) {
	for _, path := range []string{"", ".", "..", "/absolute", "../escape", "a/../b", "a/./b", "a//b", "a/", `a\b`, "a\x00b", "a:b", "C:/file"} {
		t.Run(fmt.Sprintf("路径_%q", path), func(t *testing.T) {
			def, files := revisionFixture()
			files[path] = []byte("内容")
			_, err := Seal(def, files)
			require.ErrorIs(t, err, fs.ErrInvalid, "文件路径必须是规范的斜线相对路径")
		})
	}
	for name, extra := range map[string]map[string][]byte{
		"祖先文件与子文件冲突": {"assets": {}, "assets/a.txt": {}},
		"多层祖先文件冲突":   {"assets": {}, "assets/nested/a.txt": {}},
		"技能目录与文件冲突":  {"skills/oncall": {}},
		"路径规范化后冲突":   {"assets/a.txt": {}, "assets/./a.txt": {}},
	} {
		t.Run(name, func(t *testing.T) {
			def, files := revisionFixture()
			for path, data := range extra {
				files[path] = data
			}
			_, err := Seal(def, files)
			require.ErrorIs(t, err, fs.ErrInvalid, "文件不能同时作为目录或路径别名")
		})
	}
}

func TestSealValidatesSkillMetadataAndDirectory(t *testing.T) {
	for name, mutate := range map[string]func(*Definition, map[string][]byte){
		"缺少技能说明": func(_ *Definition, f map[string][]byte) { delete(f, "skills/oncall/SKILL.md") },
		"技能名称不匹配": func(_ *Definition, f map[string][]byte) {
			f["skills/oncall/SKILL.md"] = []byte("---\nname: another\ndescription: 有效说明\n---\n")
		},
		"缺少必填描述": func(_ *Definition, f map[string][]byte) {
			f["skills/oncall/SKILL.md"] = []byte("---\nname: oncall\n---\n")
		},
		"非法说明格式":   func(_ *Definition, f map[string][]byte) { f["skills/oncall/SKILL.md"] = []byte("没有 frontmatter") },
		"技能目录穿越":   func(d *Definition, _ map[string][]byte) { d.Skills["oncall"] = "../outside" },
		"技能目录绝对路径": func(d *Definition, _ map[string][]byte) { d.Skills["oncall"] = "/outside" },
		"技能目录反斜线":  func(d *Definition, _ map[string][]byte) { d.Skills["oncall"] = `skills\oncall` },
	} {
		t.Run(name, func(t *testing.T) {
			def, files := revisionFixture()
			mutate(&def, files)
			_, err := Seal(def, files)
			require.ErrorIs(t, err, fs.ErrInvalid, "技能必须通过解析且声明名称匹配")
		})
	}
}

func TestSealEnforcesLimits(t *testing.T) {
	t.Run("路径字节数", func(t *testing.T) {
		def, files := revisionFixture()
		files[strings.Repeat("x", constants.MaxRevisionPathBytes)] = []byte{}
		_, err := Seal(def, files)
		require.NoError(t, err, "精确等于路径字节上限应可封存")
		files[strings.Repeat("x", constants.MaxRevisionPathBytes+1)] = []byte{}
		_, err = Seal(def, files)
		require.ErrorIs(t, err, fs.ErrInvalid, "超过路径字节上限应被拒绝")
	})
	t.Run("总归档预算", func(t *testing.T) {
		def, files := revisionFixture()
		// 复用同一切片，要求总量检查在深复制和 base64 编码之前生效。
		data := make([]byte, constants.MaxRevisionFileBytes)
		for i := range 4 {
			files[fmt.Sprintf("large-%d.bin", i)] = data
		}
		_, err := Seal(def, files)
		require.ErrorIs(t, err, fs.ErrInvalid, "各文件均合法但总归档超限时应拒绝")
	})
	t.Run("文件数量", func(t *testing.T) {
		def, _ := revisionFixture()
		def.Skills = nil
		files := make(map[string][]byte, constants.MaxRevisionFiles+1)
		for i := range constants.MaxRevisionFiles + 1 {
			files[fmt.Sprintf("file-%05d", i)] = []byte{}
		}
		_, err := Seal(def, files)
		require.ErrorIs(t, err, fs.ErrInvalid, "超过文件数量上限应被拒绝")
	})
	t.Run("单文件字节数", func(t *testing.T) {
		def, files := revisionFixture()
		files["large.bin"] = make([]byte, constants.MaxRevisionFileBytes+1)
		_, err := Seal(def, files)
		require.ErrorIs(t, err, fs.ErrInvalid, "超过单文件字节上限应被拒绝")
	})
	t.Run("定义字节数", func(t *testing.T) {
		def, files := revisionFixture()
		def.Prompts["system"] = strings.Repeat("x", constants.MaxRevisionDefinitionBytes+1)
		_, err := Seal(def, files)
		require.ErrorIs(t, err, fs.ErrInvalid, "提示词同样计入定义大小")
	})
	t.Run("JSON深度", func(t *testing.T) {
		def, files := revisionFixture()
		def.Config = json.RawMessage(`{"nested":` + strings.Repeat("[", constants.MaxRevisionJSONDepth+1) + "0" + strings.Repeat("]", constants.MaxRevisionJSONDepth+1) + "}")
		_, err := Seal(def, files)
		require.ErrorIs(t, err, fs.ErrInvalid, "超过 JSON 深度上限应被拒绝")
	})
}

func TestVerifyRejectsTampering(t *testing.T) {
	for name, mutate := range map[string]func(*AgentRevision){
		"标识为空":  func(r *AgentRevision) { r.ID = "" },
		"标识大小写": func(r *AgentRevision) { r.ID = strings.ToUpper(r.ID) },
		"标识不匹配": func(r *AgentRevision) { r.ID = strings.Repeat("0", 64) },
		"配置":    func(r *AgentRevision) { r.Definition.Config = json.RawMessage(`{"changed":true}`) },
		"提示词":   func(r *AgentRevision) { r.Definition.Prompts["system"] = "新提示词" },
		"工具":    func(r *AgentRevision) { r.Definition.Tools = json.RawMessage(`[]`) },
		"模型":    func(r *AgentRevision) { r.Definition.Models["primary"] = Reference{ID: "model-a", Version: "v2"} },
		"策略":    func(r *AgentRevision) { r.Definition.Policy.Version = "v2" },
		"运行时摘要": func(r *AgentRevision) { r.Definition.Runtime.Digest = strings.Repeat("b", 64) },
		"文件内容":  func(r *AgentRevision) { r.Files["skills/oncall/programs/check.bin"][0]++ },
		"删除文件":  func(r *AgentRevision) { delete(r.Files, "skills/oncall/templates/report.txt") },
		"添加文件":  func(r *AgentRevision) { r.Files["extra.txt"] = []byte("新内容") },
		"无效技能":  func(r *AgentRevision) { r.Definition.Skills["oncall"] = "missing" },
	} {
		t.Run(name, func(t *testing.T) {
			r := sealFixture(t)
			mutate(&r)
			require.ErrorIs(t, Verify(r), fs.ErrInvalid, "篡改后的修订不得通过验证")
		})
	}
}
