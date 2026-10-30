package preset

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/kart-io/goagent/harness/env"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/tools"
	"github.com/kart-io/goagent/tools/policy"
	"gopkg.in/yaml.v3"
)

// ComponentSkills 是技能组件名。
const ComponentSkills = "skills"

// ToolLoadSkill 是加载技能说明的工具名。
const ToolLoadSkill = "load_skill"

// SkillsConfig 是 skills 组件的配置。技能是目录下的 <名称>/SKILL.md，开头为 YAML frontmatter（name、description）。
// 系统提示词只列出名称与描述，模型需要时用 load_skill 读取完整说明与目录中的其他文件。
type SkillsConfig struct {
	// Dirs 是技能目录，默认 [.agents/skills, .claude/skills]；相对路径相对工作区根目录，
	// 绝对路径或 ~ 开头的目录在工作区之外，来自不可信层时需要授权。同名技能以先出现的目录为准。
	Dirs []string `yaml:"dirs"`
}

var skillNamePattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// skill 是 Build 时读取的一个技能。
type skill struct {
	name, description, body, dir string
	hidden                       bool
}

func init() {
	Register(Spec[SkillsConfig]{
		Name: ComponentSkills, Version: "1", Singleton: true,
		Validate: func(c *SkillsConfig) error {
			if len(c.Dirs) == 0 {
				c.Dirs = []string{".agents/skills", ".claude/skills"}
			}
			for _, d := range c.Dirs {
				if strings.TrimSpace(d) == "" {
					return errors.New("技能目录不能为空")
				}
			}
			return nil
		},
		Needs: func(c *SkillsConfig) Needs {
			var n Needs
			for _, d := range c.Dirs {
				if outsideWorkspace(d) {
					n.Grants = append(n.Grants, "读取工作区外的技能目录 "+d)
				}
			}
			return n
		},
		Build: buildSkills,
	})
}

func outsideWorkspace(dir string) bool { return filepath.IsAbs(dir) || strings.HasPrefix(dir, "~") }

func buildSkills(_ context.Context, a *Assembler, c *SkillsConfig) error {
	var skills []skill
	for _, d := range c.Dirs {
		dir, err := skillDir(a.Root(), d)
		if err != nil {
			return err
		}
		found, err := readSkills(dir)
		if err != nil {
			return err
		}
		for _, s := range found {
			if !slices.ContainsFunc(skills, func(x skill) bool { return x.name == s.name }) {
				skills = append(skills, s)
			}
		}
	}
	if len(skills) == 0 {
		return nil
	}
	var sb strings.Builder
	sb.WriteString("可用技能（需要时用 " + ToolLoadSkill + " 加载完整说明后再按说明操作）：\n")
	var names []string
	for _, s := range skills {
		names = append(names, s.name+"@"+sha256Hex(s.body)[:12])
		if !s.hidden {
			fmt.Fprintf(&sb, "- %s：%s\n", s.name, s.description)
		}
	}
	a.AddPrompt(strings.TrimSuffix(sb.String(), "\n"))
	a.AddTools(loadSkillTool(skills))
	a.AddRules([]policy.Rule{{Tool: ToolLoadSkill, Decision: policy.Allow}}, nil, false)
	a.SetInfo("skills", strings.Join(names, ","))
	return nil
}

func skillDir(root, d string) (string, error) {
	switch {
	case strings.HasPrefix(d, "~"):
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, strings.TrimPrefix(d, "~")), nil
	case filepath.IsAbs(d):
		return d, nil
	case root == "":
		return "", fmt.Errorf("相对技能目录 %s 需要 Host.Root", d)
	}
	// 相对目录须仍在工作区内，经 LocalFS 解析符号链接后检查。
	local, err := env.NewLocalFS(root)
	if err != nil {
		return "", err
	}
	rel, err := local.Resolve(d)
	if err != nil {
		return "", fmt.Errorf("技能目录 %s: %w", d, err)
	}
	return filepath.Join(local.Root(), filepath.FromSlash(rel)), nil
}

// readSkills 读取 dir 下每个子目录的 SKILL.md；目录不存在时返回空。
func readSkills(dir string) ([]skill, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []skill
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		path := filepath.Join(dir, e.Name(), "SKILL.md")
		data, err := os.ReadFile(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		s, err := parseSkill(data)
		if err != nil {
			return nil, fmt.Errorf("技能 %s: %w", path, err)
		}
		if s.name != e.Name() {
			return nil, fmt.Errorf("技能 %s: name %q 与目录名不一致", path, s.name)
		}
		s.dir = filepath.Join(dir, e.Name())
		out = append(out, s)
	}
	return out, nil
}

func parseSkill(data []byte) (skill, error) {
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	rest, ok := bytes.CutPrefix(data, []byte("---\n"))
	if !ok {
		return skill{}, errors.New("缺少 YAML frontmatter")
	}
	front, body, ok := bytes.Cut(rest, []byte("\n---"))
	if !ok {
		return skill{}, errors.New("frontmatter 未结束")
	}
	var meta struct {
		Name                   string `yaml:"name"`
		Description            string `yaml:"description"`
		DisableModelInvocation bool   `yaml:"disable-model-invocation"`
	}
	if err := yaml.Unmarshal(front, &meta); err != nil {
		return skill{}, err
	}
	if !skillNamePattern.MatchString(meta.Name) {
		return skill{}, fmt.Errorf("name %q 须为小写字母、数字与连字符", meta.Name)
	}
	if strings.TrimSpace(meta.Description) == "" {
		return skill{}, errors.New("缺少 description")
	}
	_, body, _ = bytes.Cut(body, []byte("\n"))
	return skill{name: meta.Name, description: strings.TrimSpace(meta.Description), body: strings.TrimSpace(string(body)), hidden: meta.DisableModelInvocation}, nil
}

// loadSkillTool 返回 load_skill：不带 file 时返回 SKILL.md 正文（Build 时读取）与目录中的其他文件，带 file 时读取该文件。
func loadSkillTool(skills []skill) interfaces.Tool {
	return tools.NewBaseTool(ToolLoadSkill,
		"加载技能的完整说明。name 为技能名；说明中提到技能目录里的其他文件时，用 file 读取（相对技能目录的路径）。",
		`{
			"type": "object",
			"properties": {
				"name": {"type": "string", "description": "技能名"},
				"file": {"type": "string", "description": "技能目录中的文件，省略时返回 SKILL.md 的说明"}
			},
			"required": ["name"],
			"additionalProperties": false
		}`,
		func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			name, _ := input.Args["name"].(string)
			i := slices.IndexFunc(skills, func(s skill) bool { return s.name == name })
			if i < 0 {
				return tools.Rejected("unknown skill "+name, nil), nil
			}
			s := skills[i]
			local, err := env.NewLocalFS(s.dir)
			if err != nil {
				return nil, err
			}
			file, _ := input.Args["file"].(string)
			if file == "" {
				var files []string
				err := local.Walk(ctx, ".", func(info env.Info) error {
					if !info.IsDir() && info.Path != "SKILL.md" {
						files = append(files, info.Path)
					}
					return nil
				})
				if err != nil {
					return nil, err
				}
				return &interfaces.ToolOutput{Success: true, Result: map[string]any{"name": s.name, "instructions": s.body, "files": files}}, nil
			}
			data, _, err := local.ReadFile(ctx, file)
			if errors.Is(err, fs.ErrNotExist) || errors.Is(err, env.ErrOutsideWorkspace) {
				return tools.Rejected(fmt.Sprintf("skill %s has no file %s", s.name, file), nil), nil
			}
			if err != nil {
				return nil, err
			}
			return &interfaces.ToolOutput{Success: true, Result: map[string]any{"name": s.name, "file": file, "content": string(data)}}, nil
		})
}
