package skill_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/constants"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/skills"
	"github.com/kart-io/goagent/tools"
	"github.com/kart-io/goagent/tools/skill"
	"github.com/stretchr/testify/require"
)

func fixture(t *testing.T) (*skills.Catalog, string) {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"example/SKILL.md":            "---\nname: example\ndescription: Test instructions\ndisable-model-invocation: true\nallowed-tools: unavailable-tool\n---\nRead references/guide.md.\n",
		"example/references/guide.md": "Reference content.\n",
		"example/scripts/run.sh":      "#!/bin/sh\nprintf executed > '" + filepath.Join(dir, "executed") + "'\n",
	}
	for name, content := range files {
		path := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o700))
	}
	root, err := os.OpenRoot(dir)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, root.Close()) })
	catalog, err := skills.Discover(context.Background(), root)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, catalog.Close()) })
	return catalog, dir
}

func input(args map[string]any) *interfaces.ToolInput {
	return &interfaces.ToolInput{Args: args}
}

func TestLoadAndReferenceContracts(t *testing.T) {
	catalog, dir := fixture(t)
	tool := skill.New(catalog)
	require.Equal(t, "load_skill", tool.Name())
	require.NotEmpty(t, tool.Description())
	// Hidden metadata and allowed-tools are host policy, not adapter filters.
	for _, args := range []map[string]any{{"name": "example"}, {"name": "example", "file": ""}} {
		out, err := tool.Invoke(context.Background(), input(args))
		require.NoError(t, err)
		require.True(t, out.Success)
		require.Equal(t, map[string]any{
			"name": "example", "instructions": "Read references/guide.md.",
			"files": []string{"references/guide.md", "scripts/run.sh"},
		}, out.Result)
		require.Empty(t, out.AdditionalContext)
	}

	for _, file := range []string{"references/guide.md", "./references/guide.md", "references/./guide.md", "references//guide.md", "scripts/run.sh"} {
		out, err := tool.Invoke(context.Background(), input(map[string]any{"name": "example", "file": file}))
		require.NoError(t, err)
		require.True(t, out.Success)
		content, err := os.ReadFile(filepath.Join(dir, "example", file))
		require.NoError(t, err)
		require.Equal(t, map[string]any{"name": "example", "file": file, "content": string(content)}, out.Result)
	}
	_, err := os.Stat(filepath.Join(dir, "executed"))
	require.ErrorIs(t, err, os.ErrNotExist, "loading or reading a script must never execute it")
	_, err = catalog.Load(context.Background(), "example")
	require.NoError(t, err, "adapter must leave catalog lifetime to its owner")
}

func TestEmptyResourcesEncodeAsArray(t *testing.T) {
	catalog, dir := fixture(t)
	for _, name := range []string{"references", "scripts"} {
		require.NoError(t, os.RemoveAll(filepath.Join(dir, "example", name)))
	}
	out, err := skill.New(catalog).Invoke(context.Background(), input(map[string]any{"name": "example"}))
	require.NoError(t, err)
	require.True(t, out.Success)
	encoded, err := json.Marshal(out.Result)
	require.NoError(t, err)
	require.JSONEq(t, `{"name":"example","instructions":"Read references/guide.md.","files":[]}`, string(encoded))
}

func TestUnknownSkillsAndInvalidReferencesAreRejected(t *testing.T) {
	catalog, dir := fixture(t)
	tool := skill.New(catalog)
	for _, name := range []string{"missing", "", "../example", "/example", "example/../example", "EXAMPLE"} {
		for _, file := range []string{"", "references/guide.md"} {
			t.Run(name+":"+file, func(t *testing.T) {
				out, err := tool.Invoke(context.Background(), input(map[string]any{"name": name, "file": file}))
				require.NoError(t, err)
				message, rejected := tools.RejectedResult(out)
				require.True(t, rejected)
				require.Contains(t, message, "unknown skill")
			})
		}
	}
	for _, file := range []string{"missing.md", "../example/SKILL.md", filepath.Join(dir, "example/SKILL.md"), "references", "bad\x00path"} {
		t.Run(file, func(t *testing.T) {
			out, err := tool.Invoke(context.Background(), input(map[string]any{"name": "example", "file": file}))
			require.NoError(t, err)
			message, rejected := tools.RejectedResult(out)
			require.True(t, rejected)
			if file == "missing.md" {
				require.Contains(t, message, "has no file")
			} else {
				require.Contains(t, message, "cannot read")
			}
		})
	}
}

func TestInvalidArgumentsUseFrameworkValidation(t *testing.T) {
	catalog, _ := fixture(t)
	tool := skill.New(catalog)
	for name, in := range map[string]*interfaces.ToolInput{
		"nil input":     nil,
		"nil args":      {},
		"missing name":  input(map[string]any{}),
		"null name":     input(map[string]any{"name": nil}),
		"number name":   input(map[string]any{"name": 42}),
		"null file":     input(map[string]any{"name": "example", "file": nil}),
		"array file":    input(map[string]any{"name": "example", "file": []string{"x"}}),
		"unknown field": input(map[string]any{"name": "example", "execute": true}),
	} {
		t.Run(name, func(t *testing.T) {
			out, err := tool.Invoke(context.Background(), in)
			require.Error(t, err)
			require.NotNil(t, out)
			require.False(t, out.Success)
			var frameworkError *agentErrors.AgentError
			require.ErrorAs(t, err, &frameworkError)
			message, marked := frameworkError.ModelArgumentMessage()
			require.True(t, marked)
			require.Equal(t, out.Error, message)
		})
	}
}

func TestCanceledAndClosedCatalogRemainErrors(t *testing.T) {
	catalog, _ := fixture(t)
	tool := skill.New(catalog)
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	expired, stop := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer stop()
	for _, ctx := range []context.Context{canceled, expired} {
		for _, name := range []string{"example", "missing"} {
			for _, file := range []string{"", "references/guide.md"} {
				out, err := tool.Invoke(ctx, input(map[string]any{"name": name, "file": file}))
				require.ErrorIs(t, err, ctx.Err())
				_, rejected := tools.RejectedResult(out)
				require.False(t, rejected)
			}
		}
	}
	require.NoError(t, catalog.Close())
	for _, name := range []string{"example", "missing"} {
		for _, file := range []string{"", "references/guide.md"} {
			out, err := tool.Invoke(context.Background(), input(map[string]any{"name": name, "file": file}))
			require.ErrorIs(t, err, os.ErrClosed)
			_, rejected := tools.RejectedResult(out)
			require.False(t, rejected, "closed catalog is an operational failure")
		}
	}
}

func TestNilCatalogFailsWithoutPanic(t *testing.T) {
	out, err := skill.New(nil).Invoke(context.Background(), input(map[string]any{"name": "example"}))
	require.Error(t, err)
	var frameworkError *agentErrors.AgentError
	require.True(t, errors.As(err, &frameworkError))
	_, rejected := tools.RejectedResult(out)
	require.False(t, rejected)
}

func TestReferenceContentAndLimitRejections(t *testing.T) {
	catalog, dir := fixture(t)
	tool := skill.New(catalog)
	raw := []byte{0x89, 'P', 'N', 'G', 0xff, 0x00}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "example", "image.png"), raw, 0600))
	data, err := catalog.ReadReference(context.Background(), "example", "image.png")
	require.NoError(t, err)
	require.Equal(t, raw, data, "raw Catalog reads preserve binary assets")
	out, err := tool.Invoke(context.Background(), input(map[string]any{"name": "example", "file": "image.png"}))
	require.NoError(t, err)
	message, rejected := tools.RejectedResult(out)
	require.True(t, rejected, "text tool must not silently corrupt bytes at the model boundary")
	require.Contains(t, message, "not UTF-8 text")

	file, err := os.Create(filepath.Join(dir, "example", "large.txt"))
	require.NoError(t, err)
	require.NoError(t, file.Truncate(constants.SkillMaxReferenceBytes+1))
	require.NoError(t, file.Close())
	out, err = tool.Invoke(context.Background(), input(map[string]any{"name": "example", "file": "large.txt"}))
	require.NoError(t, err)
	message, rejected = tools.RejectedResult(out)
	require.True(t, rejected)
	require.Contains(t, message, "byte limit")
	require.NotContains(t, message, "has no file")
}
