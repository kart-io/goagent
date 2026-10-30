package preset

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

type fileFormat struct {
	Limits  *limitsFile  `yaml:"limits"`
	Presets []presetFile `yaml:"presets"`
}

type presetFile struct {
	ID          string          `yaml:"id"`
	Name        *string         `yaml:"name"`
	Description *string         `yaml:"description"`
	Remove      bool            `yaml:"remove"`
	Components  []componentFile `yaml:"components"`
}

type componentFile struct {
	ID      string    `yaml:"id"`
	Use     string    `yaml:"use"`
	Config  yaml.Node `yaml:"config"`
	Remove  bool      `yaml:"remove"`
	Replace bool      `yaml:"replace"`
	Locked  bool      `yaml:"locked"`
}

// Set 是合并各层后的全部 preset。
type Set struct {
	limits  Limits
	presets []*Resolved
	removed []RemovedPreset
}

// RemovedPreset 是被某层删除的 preset，供 Dump 展示。
type RemovedPreset struct {
	ID        string
	RemovedBy Origin
}

// Resolved 是合并、校验后的一个 preset。
type Resolved struct {
	ID, Name, Description string
	Origin                Origin
	// Rows 是最终组件列表，顺序即提示词段落与钩子的顺序。
	Rows []*Row
	// Removed 是被删除的组件行，供 Dump 展示。
	Removed []*Row
	// Limits 是可信层声明的权限上限。
	Limits Limits
	// Expansions 是需要宿主授权的扩权项。
	Expansions []Expansion

	removals []Expansion
	set      *Set
}

// Row 是一行组件配置。
type Row struct {
	ID, Use string
	Locked  bool
	Origin  Origin
	// Overrides 是被本行替换的早先定义的来源，按时间顺序。
	Overrides []Origin
	// RemovedBy 只在被删除的行上设置。
	RemovedBy *Origin

	raw             *yaml.Node
	config          any
	comp            *component
	replacedTrusted bool
}

// Expansion 是一项需要授权的扩权：来源不可信的配置放宽了权限。
type Expansion struct {
	Preset, Component string
	Origin            Origin
	Reason            string
}

func (e Expansion) String() string {
	return fmt.Sprintf("preset %s 组件 %s：%s（%s）", e.Preset, e.Component, e.Reason, e.Origin)
}

// Load 按顺序叠加各层配置并校验。Load 不启动进程、不连网络、不读取秘密的值。
func Load(sources ...Source) (*Set, error) {
	set := &Set{limits: defaultLimits()}
	for _, src := range sources {
		origin := Origin{Layer: src.Layer, Path: src.Path, Trusted: src.Trusted}
		if err := set.apply(src, origin); err != nil {
			return nil, fmt.Errorf("%s: %w", origin, err)
		}
	}
	for _, p := range set.presets {
		p.Limits = set.limits
		p.set = set
		if err := p.validate(); err != nil {
			return nil, fmt.Errorf("preset %s: %w", p.ID, err)
		}
	}
	if err := set.checkReferences(); err != nil {
		return nil, err
	}
	return set, nil
}

// checkReferences 检查组件引用的 preset 存在且引用关系没有环。
func (s *Set) checkReferences() error {
	state := map[string]int{} // 1 访问中，2 完成
	var visit func(p *Resolved, path []string) error
	visit = func(p *Resolved, path []string) error {
		switch state[p.ID] {
		case 1:
			return fmt.Errorf("preset 引用有环：%s", strings.Join(append(path, p.ID), " → "))
		case 2:
			return nil
		}
		state[p.ID] = 1
		for _, r := range p.Rows {
			if r.comp.references == nil {
				continue
			}
			for _, id := range r.comp.references(r.config) {
				target, ok := s.Get(id)
				if !ok {
					return fmt.Errorf("preset %s 组件 %s 引用的 preset %s 不存在", p.ID, r.ID, id)
				}
				if err := visit(target, append(path, p.ID)); err != nil {
					return err
				}
			}
		}
		state[p.ID] = 2
		return nil
	}
	for _, p := range s.presets {
		if err := visit(p, nil); err != nil {
			return err
		}
	}
	return nil
}

func (s *Set) apply(src Source, origin Origin) error {
	dec := yaml.NewDecoder(bytes.NewReader(src.Data))
	dec.KnownFields(true)
	var f fileFormat
	if err := dec.Decode(&f); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if f.Limits != nil {
		if !src.Trusted {
			return errors.New("limits 只能由可信层声明")
		}
		if err := f.Limits.apply(&s.limits); err != nil {
			return err
		}
	}
	seen := map[string]bool{}
	for _, pf := range f.Presets {
		if pf.ID == "" {
			return errors.New("preset 缺少 id")
		}
		if seen[pf.ID] {
			return fmt.Errorf("preset %s 在同一层中重复", pf.ID)
		}
		seen[pf.ID] = true
		if err := s.applyPreset(pf, origin); err != nil {
			return fmt.Errorf("preset %s: %w", pf.ID, err)
		}
	}
	return nil
}

func (s *Set) applyPreset(pf presetFile, origin Origin) error {
	i := slices.IndexFunc(s.presets, func(p *Resolved) bool { return p.ID == pf.ID })
	if pf.Remove {
		if i < 0 {
			return errors.New("要删除的 preset 不存在")
		}
		if !origin.Trusted && slices.ContainsFunc(s.presets[i].Rows, func(r *Row) bool { return r.Locked }) {
			return errors.New("含锁定组件的 preset 只能由可信层删除")
		}
		s.presets = slices.Delete(s.presets, i, i+1)
		s.removed = append(s.removed, RemovedPreset{ID: pf.ID, RemovedBy: origin})
		return nil
	}
	var p *Resolved
	if i < 0 {
		p = &Resolved{ID: pf.ID, Origin: origin}
		s.presets = append(s.presets, p)
	} else {
		p = s.presets[i]
	}
	if pf.Name != nil {
		p.Name = *pf.Name
	}
	if pf.Description != nil {
		p.Description = *pf.Description
	}
	ids := map[string]bool{}
	for _, cf := range pf.Components {
		if cf.ID == "" {
			return errors.New("组件缺少 id")
		}
		if ids[cf.ID] {
			return fmt.Errorf("组件 %s 在同一层中重复", cf.ID)
		}
		ids[cf.ID] = true
		if err := p.applyRow(cf, origin); err != nil {
			return fmt.Errorf("组件 %s: %w", cf.ID, err)
		}
	}
	return nil
}

func (p *Resolved) applyRow(cf componentFile, origin Origin) error {
	if cf.Locked && !origin.Trusted {
		return errors.New("只有可信层可以锁定组件")
	}
	i := slices.IndexFunc(p.Rows, func(r *Row) bool { return r.ID == cf.ID })
	if cf.Remove {
		if i < 0 {
			return errors.New("要删除的组件不存在")
		}
		old := p.Rows[i]
		if old.Locked && !origin.Trusted {
			return errors.New("组件已被可信层锁定，不能删除")
		}
		if old.Origin.Trusted && !origin.Trusted {
			p.removals = append(p.removals, Expansion{Preset: p.ID, Component: old.ID, Origin: origin, Reason: "删除来自可信层的组件"})
		}
		removedBy := origin
		old.RemovedBy = &removedBy
		p.Removed = append(p.Removed, old)
		p.Rows = slices.Delete(p.Rows, i, i+1)
		return nil
	}
	if cf.Use == "" {
		return errors.New("组件缺少 use")
	}
	config := cf.Config
	row := &Row{ID: cf.ID, Use: cf.Use, Locked: cf.Locked, Origin: origin, raw: &config}
	if i < 0 {
		if cf.Replace {
			return errors.New("replace 只能用于替换已有组件")
		}
		p.Rows = append(p.Rows, row)
		return nil
	}
	old := p.Rows[i]
	if old.Locked && !origin.Trusted {
		return errors.New("组件已被可信层锁定，不能替换")
	}
	if old.Use != cf.Use && !cf.Replace {
		return fmt.Errorf("不能把 use 从 %s 改为 %s；确需更换实现时写 replace: true", old.Use, cf.Use)
	}
	row.Overrides = append(slices.Clone(old.Overrides), old.Origin)
	row.replacedTrusted = !origin.Trusted && (old.Origin.Trusted || old.replacedTrusted)
	p.Rows[i] = row
	return nil
}

// validate 解码组件配置并检查单例、依赖与扩权项。
func (p *Resolved) validate() error {
	counts := map[string]int{}
	for _, r := range p.Rows {
		r.comp = lookup(r.Use)
		if r.comp == nil {
			return fmt.Errorf("组件 %s: 未知的组件 %q", r.ID, r.Use)
		}
		cfg := r.comp.newConfig()
		if r.raw.Kind != 0 {
			data, err := yaml.Marshal(r.raw)
			if err != nil {
				return fmt.Errorf("组件 %s: %w", r.ID, err)
			}
			dec := yaml.NewDecoder(bytes.NewReader(data))
			dec.KnownFields(true)
			if err := dec.Decode(cfg); err != nil {
				return fmt.Errorf("组件 %s 配置: %w", r.ID, err)
			}
		}
		if err := r.comp.validate(cfg); err != nil {
			return fmt.Errorf("组件 %s 配置: %w", r.ID, err)
		}
		for _, name := range secretsIn(cfg) {
			if name == "" {
				return fmt.Errorf("组件 %s 配置: secret 引用缺少名称", r.ID)
			}
		}
		r.config = cfg
		counts[r.Use]++
	}
	for _, r := range p.Rows {
		if r.comp.singleton && counts[r.Use] > 1 {
			return fmt.Errorf("组件 %s 在一个 preset 中只能有一行", r.Use)
		}
		for _, req := range r.comp.requires {
			if counts[req] == 0 {
				return fmt.Errorf("组件 %s 需要 %s 组件", r.ID, req)
			}
		}
	}
	cfgs := make([]any, len(p.Rows))
	for i, r := range p.Rows {
		cfgs[i] = r.config
	}
	p.Expansions = p.expansions(cfgs, nil, "")
	return nil
}

// expansions 计算扩权项。cfgs 是各行的有效配置；contentNeeds 是 Resolve 从工作区内容得到的需求，按不可信处理。
func (p *Resolved) expansions(cfgs []any, contentNeeds []Needs, root string) []Expansion {
	var out []Expansion
	for i, r := range p.Rows {
		if r.replacedTrusted {
			out = append(out, Expansion{Preset: p.ID, Component: r.ID, Origin: r.Origin, Reason: "替换来自可信层的组件"})
		}
		if !r.Origin.Trusted {
			for _, reason := range p.Limits.exceeded(r.comp.needs(cfgs[i]), secretsIn(cfgs[i])) {
				out = append(out, Expansion{Preset: p.ID, Component: r.ID, Origin: r.Origin, Reason: reason})
			}
		}
		if i < len(contentNeeds) {
			workspace := Origin{Layer: "workspace", Path: root}
			for _, reason := range p.Limits.exceeded(contentNeeds[i], nil) {
				out = append(out, Expansion{Preset: p.ID, Component: r.ID, Origin: workspace, Reason: reason})
			}
		}
	}
	for _, e := range p.removals {
		// 被可信层重新加入的组件不再算作删除。
		if !slices.ContainsFunc(p.Rows, func(r *Row) bool { return r.ID == e.Component && r.Origin.Trusted }) {
			out = append(out, e)
		}
	}
	return out
}

// Get 返回 id 对应的 preset。
func (s *Set) Get(id string) (*Resolved, bool) {
	i := slices.IndexFunc(s.presets, func(p *Resolved) bool { return p.ID == id })
	if i < 0 {
		return nil, false
	}
	return s.presets[i], true
}

// Presets 返回全部 preset，按首次定义的顺序。
func (s *Set) Presets() []*Resolved { return slices.Clone(s.presets) }
