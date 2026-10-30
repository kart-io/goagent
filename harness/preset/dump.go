package preset

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Dump 输出合并结果：权限上限、每个 preset 的最终组件（含来源、被覆盖的来源与配置）、被删除的组件与 preset，
// 以及需要授权的扩权项。配置中的秘密只显示引用。
func (s *Set) Dump() string {
	var sb strings.Builder
	l := s.limits
	fmt.Fprintf(&sb, "limits: sandbox=%s executors=%s images=%s network=%v hostProcesses=%v secrets=%v\n",
		l.Sandbox, listOrAny(l.Executors), listOrAny(l.Images), l.Network, l.HostProcesses, l.Secrets)
	for _, p := range s.presets {
		fmt.Fprintf(&sb, "\npreset %s（首次定义：%s）\n", p.ID, p.Origin)
		if p.Name != "" {
			fmt.Fprintf(&sb, "  name: %s\n", p.Name)
		}
		if p.Description != "" {
			fmt.Fprintf(&sb, "  description: %s\n", p.Description)
		}
		for _, r := range p.Rows {
			fmt.Fprintf(&sb, "  - %s [%s] 来源 %s", r.ID, r.Use, r.Origin)
			if r.Locked {
				sb.WriteString(" 已锁定")
			}
			sb.WriteString("\n")
			for _, o := range r.Overrides {
				fmt.Fprintf(&sb, "      覆盖了 %s\n", o)
			}
			if cfg, err := rawJSON(r); err == nil && cfg != nil {
				data, _ := json.Marshal(cfg)
				fmt.Fprintf(&sb, "      config: %s\n", data)
			}
		}
		for _, r := range p.Removed {
			fmt.Fprintf(&sb, "  - （已删除）%s [%s] 来源 %s，删除者 %s\n", r.ID, r.Use, r.Origin, r.RemovedBy)
		}
		if len(p.Expansions) > 0 {
			sb.WriteString("  需要授权：\n")
			for _, e := range p.Expansions {
				fmt.Fprintf(&sb, "    - %s：%s（%s）\n", e.Component, e.Reason, e.Origin)
			}
		}
	}
	for _, r := range s.removed {
		fmt.Fprintf(&sb, "\n（已删除）preset %s，删除者 %s\n", r.ID, r.RemovedBy)
	}
	return sb.String()
}

func listOrAny(v []string) string {
	if v == nil {
		return "不限"
	}
	return fmt.Sprint(v)
}
