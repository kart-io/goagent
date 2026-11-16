package memorytest_test

import (
	"testing"

	"github.com/kart-io/goagent/memory"
	"github.com/kart-io/goagent/memory/memorytest"
)

func TestMemoryIndex(t *testing.T) {
	memorytest.RunIndex(t, func(t *testing.T) memory.Index { return memory.NewMemoryIndex() })
}
