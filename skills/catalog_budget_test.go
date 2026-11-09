package skills_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/kart-io/goagent/constants"
	agenterrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/skills"
	"github.com/stretchr/testify/require"
)

func writeBudgetFiles(t *testing.T, dir string, count int) []string {
	t.Helper()
	names := make([]string, 0, count)
	for i := range count {
		name := fmt.Sprintf("resource-%05d.txt", i)
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("reference"), 0600))
		names = append(names, name)
	}
	return names
}

func requireBudgetError(t *testing.T, err error, message string) {
	t.Helper()
	require.ErrorContains(t, err, message)
	require.Equal(t, agenterrors.CodeInvalidInput, agenterrors.GetCode(err))
}

func TestCatalogBudgetDiscoverEntriesAcrossSources(t *testing.T) {
	ctx := context.Background()
	first, second := t.TempDir(), t.TempDir()
	writeSkill(t, first, "first", "first", "first instructions")
	writeSkill(t, second, "second", "second", "second instructions")
	// 普通文件和无 SKILL.md 的目录同样消耗发现预算，不能只数技能。
	require.NoError(t, os.Mkdir(filepath.Join(first, "without-skill"), 0700))
	firstEntries := constants.SkillMaxEntries / 2
	writeBudgetFiles(t, first, firstEntries-2)
	writeBudgetFiles(t, second, constants.SkillMaxEntries-firstEntries-1)
	r1, r2 := openRoot(t, first), openRoot(t, second)
	c := discover(t, r1, r2)
	list := c.List()
	require.Len(t, list, 2)
	require.Equal(t, "first", list[0].Name)
	require.Equal(t, "second", list[1].Name)
	require.NoError(t, c.Close())

	// 每个来源仍低于上限，只有跨来源累计才能拒绝第 10001 个条目。
	require.NoError(t, os.WriteFile(filepath.Join(second, "overflow.txt"), nil, 0600))
	partial, err := skills.Discover(ctx, r1, r2)
	if partial != nil {
		t.Cleanup(func() { require.NoError(t, partial.Close()) })
	}
	requireBudgetError(t, err, "directory entry limit exceeded")
	require.Nil(t, partial, "后续来源超限时，不得返回先前已发现的技能")
}

func TestCatalogBudgetResourceEntriesAcrossDirectories(t *testing.T) {
	base := t.TempDir()
	dir := writeSkill(t, base, "demo", "demo", "instructions")
	for _, subdir := range []string{"a", "b"} {
		require.NoError(t, os.Mkdir(filepath.Join(dir, subdir), 0700))
	}
	// 根目录的 SKILL.md 和两个子目录占三个条目，均不进入 Files。
	fileCount := constants.SkillMaxEntries - 3
	var expected []string
	for _, name := range writeBudgetFiles(t, filepath.Join(dir, "a"), fileCount/2) {
		expected = append(expected, "a/"+name)
	}
	for _, name := range writeBudgetFiles(t, filepath.Join(dir, "b"), fileCount-fileCount/2) {
		expected = append(expected, "b/"+name)
	}
	c := discover(t, openRoot(t, base))
	ctx := context.Background()
	for range 2 {
		loaded, err := c.Load(ctx, "demo")
		require.NoError(t, err)
		require.Equal(t, "instructions", loaded.Body)
		require.Equal(t, expected, loaded.Files, "恰好达到预算时返回全部资源，每次 Load 独立计数")
	}

	// a 已产生文件结果后，b 耗尽整个资源树的共享预算。
	overflow := filepath.Join(dir, "b", "overflow.txt")
	require.NoError(t, os.WriteFile(overflow, nil, 0600))
	partial, err := c.Load(ctx, "demo")
	requireBudgetError(t, err, "directory entry limit exceeded")
	require.Equal(t, skills.Skill{}, partial, "超限不能返回部分正文、元数据或文件列表")
	require.NoError(t, os.Remove(overflow))
	loaded, err := c.Load(ctx, "demo")
	require.NoError(t, err)
	require.Equal(t, expected, loaded.Files, "超限失败不得污染后续枚举预算")
}

func TestCatalogBudgetUniqueSkillsAcrossSources(t *testing.T) {
	ctx := context.Background()
	first, second := t.TempDir(), t.TempDir()
	expected := make(map[string]string, constants.SkillMaxCount)
	for i := range constants.SkillMaxCount {
		name := fmt.Sprintf("skill-%04d", i)
		root := first
		if i >= constants.SkillMaxCount/2 {
			root = second
		}
		writeSkill(t, root, name, "selected", "selected instructions")
		expected[name] = "selected"
	}
	// 多解析一个同名文档，但选中数量仍为 2000，且第一来源优先。
	writeSkill(t, second, "skill-0000", "shadowed", "shadowed instructions")
	r1, r2 := openRoot(t, first), openRoot(t, second)
	c, err := skills.Discover(ctx, r1, r2)
	if c != nil {
		t.Cleanup(func() { require.NoError(t, c.Close()) })
	}
	skipInsufficientBudgetHandles(t, err)
	require.NoError(t, err)
	list := c.List()
	require.Len(t, list, constants.SkillMaxCount)
	actual := make(map[string]string, len(list))
	for _, m := range list {
		actual[m.Name] = m.Description
	}
	require.Equal(t, expected, actual)
	loaded, err := c.Load(ctx, "skill-0000")
	require.NoError(t, err)
	require.Equal(t, "selected instructions", loaded.Body)
	require.NoError(t, c.Close())

	writeSkill(t, second, fmt.Sprintf("skill-%04d", constants.SkillMaxCount), "overflow", "overflow instructions")
	partial, err := skills.Discover(ctx, r1, r2)
	if partial != nil {
		t.Cleanup(func() { require.NoError(t, partial.Close()) })
	}
	skipInsufficientBudgetHandles(t, err)
	requireBudgetError(t, err, "skill count limit exceeded")
	require.Nil(t, partial, "第 2001 个唯一技能超限后不得返回部分 Catalog")
}

func skipInsufficientBudgetHandles(t *testing.T, err error) {
	t.Helper()
	if errors.Is(err, syscall.EMFILE) || errors.Is(err, syscall.ENFILE) {
		t.Skipf("环境句柄不足，无法验证同时持有 %d 个技能目录的数量边界；未将句柄耗尽算作数量限制通过：%v", constants.SkillMaxCount+1, err)
	}
}

func TestCatalogBudgetEmptyFilesNonNil(t *testing.T) {
	base := t.TempDir()
	writeSkill(t, base, "demo", "demo", "instructions")
	c := discover(t, openRoot(t, base))
	loaded, err := c.Load(context.Background(), "demo")
	require.NoError(t, err)
	require.Equal(t, "instructions", loaded.Body)
	require.Equal(t, "demo", loaded.Metadata.Name)
	require.NotNil(t, loaded.Files, "只有 SKILL.md 时 Files 必须为非 nil 空切片")
	require.Empty(t, loaded.Files)
}
