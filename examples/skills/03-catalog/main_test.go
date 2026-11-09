package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRunReturnsToolRejection(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, "faq"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "faq", "SKILL.md"), []byte("---\nname: faq\ndescription: demo\n---\nRead references/refunds.md"), 0600))
	err := run(dir)
	require.ErrorContains(t, err, "load_skill rejected")
	require.ErrorContains(t, err, "has no file")
}

func TestRunExampleFixture(t *testing.T) {
	require.NoError(t, run("fixtures"))
}
