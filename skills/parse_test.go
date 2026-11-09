package skills_test

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/constants"
	agenterrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/skills"
)

func TestParseMetadataAndExactDigest(t *testing.T) {
	doc := "---\nname: oncall\ndescription: |\n  Diagnose a service.\n  Read the runbook.\nlicense: MIT\ncompatibility: Needs logs\nmetadata:\n  version: \"1.2\"\n  owner: platform\nallowed-tools: Read Bash(git:*)\ndisable-model-invocation: true\nvendor-field: ignored\n---\n\n# Diagnose\n\nRead references/runbook.md.\n"
	for _, raw := range []string{doc, "\ufeff" + strings.ReplaceAll(doc, "\n", "\r\n")} {
		s, err := skills.Parse([]byte(raw))
		require.NoError(t, err)
		require.Equal(t, "oncall", s.Metadata.Name)
		require.Contains(t, s.Metadata.Description, "Diagnose a service.")
		require.Equal(t, "MIT", s.Metadata.License)
		require.Equal(t, "Needs logs", s.Metadata.Compatibility)
		require.Equal(t, "1.2", s.Metadata.Version)
		require.Equal(t, "platform", s.Metadata.Attributes["owner"])
		require.Equal(t, "Read Bash(git:*)", s.Metadata.AllowedTools)
		require.True(t, s.Metadata.DisableModelInvocation)
		require.Equal(t, fmt.Sprintf("%x", sha256.Sum256([]byte(raw))), s.Metadata.Digest)
		require.Empty(t, s.Metadata.Source)
		require.Equal(t, "# Diagnose\n\nRead references/runbook.md.", strings.ReplaceAll(s.Body, "\r\n", "\n"))
		require.Empty(t, s.Files)
	}
}

func TestParseRejectsMalformedOrInvalidSkills(t *testing.T) {
	valid := "name: demo\ndescription: useful\n"
	for name, raw := range map[string]string{
		"missing frontmatter":    "# Hello",
		"missing delimiter":      "---\n" + valid,
		"delimiter prefix":       "---\n" + valid + "---not-a-delimiter\nbody",
		"malformed YAML":         "---\nname: [\n---\n",
		"duplicate name":         "---\n" + valid + "name: other\n---\n",
		"wrong root":             "---\n- name: demo\n---\n",
		"numeric name":           "---\nname: 123\ndescription: useful\n---\n",
		"uppercase":              "---\nname: Demo\ndescription: useful\n---\n",
		"double hyphen":          "---\nname: demo--tool\ndescription: useful\n---\n",
		"name too long":          "---\nname: " + strings.Repeat("a", 65) + "\ndescription: useful\n---\n",
		"description missing":    "---\nname: demo\n---\n",
		"description empty":      "---\nname: demo\ndescription: '  '\n---\n",
		"description too long":   "---\nname: demo\ndescription: " + strings.Repeat("界", 1025) + "\n---\n",
		"compatibility empty":    "---\n" + valid + "compatibility: ''\n---\n",
		"compatibility too long": "---\n" + valid + "compatibility: " + strings.Repeat("a", 501) + "\n---\n",
		"numeric metadata":       "---\n" + valid + "metadata: {version: 1}\n---\n",
		"二进制说明":                  "---\nname: demo\ndescription: !!binary /w==\n---\n",
		"二进制元数据":                 "---\n" + valid + "metadata: {version: !!binary MQ==}\n---\n",
		"numeric metadata key":   "---\n" + valid + "metadata: {1: value}\n---\n",
		"null metadata value":    "---\n" + valid + "metadata: {version: null}\n---\n",
		"null metadata":          "---\n" + valid + "metadata: null\n---\n",
		"null compatibility":     "---\n" + valid + "compatibility: null\n---\n",
		"null name":              "---\nname: null\ndescription: useful\n---\n",
		"null boolean":           "---\n" + valid + "disable-model-invocation: null\n---\n",
		"YAML11 boolean":         "---\n" + valid + "disable-model-invocation: yes\n---\n",
		"invalid boolean":        "---\n" + valid + "disable-model-invocation: 'false'\n---\n",
		"invalid utf8":           "---\n" + valid + "---\n\xff",
		"over byte limit":        strings.Repeat("x", constants.SkillMaxDocumentBytes+1),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := skills.Parse([]byte(raw))
			require.Error(t, err)
			require.Equal(t, agenterrors.CodeInvalidInput, agenterrors.GetCode(err))
		})
	}
}

func TestParseBoundsAliasExpansion(t *testing.T) {
	var doc strings.Builder
	doc.WriteString("---\nname: demo\ndescription: useful\nmetadata:\n  base: &large ")
	doc.WriteString(strings.Repeat("x", 65536))
	doc.WriteByte('\n')
	for i := range 128 {
		fmt.Fprintf(&doc, "  ref%d: *large\n", i)
	}
	doc.WriteString("---\nbody")
	require.Less(t, doc.Len(), constants.SkillMaxDocumentBytes)
	_, err := skills.Parse([]byte(doc.String()))
	require.Error(t, err, "small raw input must not expand into unbounded metadata")
	s, err := skills.Parse([]byte("---\nname: demo\ndescription: &desc useful\nmetadata: {summary: *desc}\n---\nbody"))
	require.NoError(t, err, "bounded aliases remain usable")
	require.Equal(t, "useful", s.Metadata.Attributes["summary"])
}

func TestParseBoundariesAndBodySeparators(t *testing.T) {
	data := "--- \r\nname: " + strings.Repeat("x", 64) + "\r\ndescription: " + strings.Repeat("界", 1024) + "\r\ncompatibility: " + strings.Repeat("a", 500) + "\r\n---\r\nfirst\n---\nlast"
	s, err := skills.Parse([]byte(data))
	require.NoError(t, err)
	require.Equal(t, "first\n---\nlast", s.Body)
	s, err = skills.Parse([]byte("---\nname: demo\ndescription: useful\n---"))
	require.NoError(t, err)
	require.Empty(t, s.Body)
}

func FuzzParse(f *testing.F) {
	f.Add([]byte("---\nname: demo\ndescription: useful\n---\nbody"))
	f.Add([]byte("\ufeff---\r\nname: demo\r\ndescription: useful\r\n---"))
	f.Fuzz(func(t *testing.T, data []byte) {
		s, err := skills.Parse(data)
		if err == nil {
			require.NotEmpty(t, s.Metadata.Name)
			require.NotEmpty(t, s.Metadata.Description)
			require.Equal(t, fmt.Sprintf("%x", sha256.Sum256(data)), s.Metadata.Digest)
		}
	})
}
