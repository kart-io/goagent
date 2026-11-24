package cache

import "github.com/kart-io/goagent/interfaces"

// Bytes is the byte-only cache contract. It is an alias of
// interfaces.ByteCache, which is covered by the stable API promise.
type Bytes = interfaces.ByteCache

var _ Bytes = (*SimpleCache)(nil)
