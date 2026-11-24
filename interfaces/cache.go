package interfaces

import (
	"context"
	"time"
)

// ByteCache is a cache that stores only bytes. External backends such as Redis
// cannot hold arbitrary Go values, so the narrow contract exposes misuse at
// compile time.
//
// Implementations: cache.SimpleCache and cache/redis in goagent-contrib.
type ByteCache interface {
	// GetBytes returns errors.ErrCacheMiss from the goagent errors package when
	// the key is absent.
	GetBytes(ctx context.Context, key string) ([]byte, error)
	SetBytes(ctx context.Context, key string, value []byte, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
}

// TaggedCache is a cache capability for deleting entries in bulk by tag. It is
// independent of ByteCache and implemented by backends that support it.
//
// Requirements:
//   - DeleteTag is idempotent: it returns nil when the tag does not exist, on
//     repeated calls, and when entries are already deleted or expired.
//   - In external backends such as Redis, a tag set must not expire before the
//     TTL of its member entries; otherwise those entries can no longer be
//     deleted by tag.
//   - When a tag has many entries, delete them in batches so a single call does
//     not block the backend for long.
//
// Consistency is defined by two properties, where "completed" means SetTagged
// returned before DeleteTag started:
//   - S1 (required): after DeleteTag returns, entries carrying the tag that were
//     written by completed SetTagged calls are not readable. Atomic operations,
//     tokens or version checks that invalidate old writes on read all qualify
//     (see cache/redis in goagent-contrib); transactions or scripts are not
//     required. Deleting more is allowed (for example a new untagged value that
//     overwrote the entry), but an overwritten tagged value must never come back.
//   - S2 (implementations must document it): when those entries are physically
//     removed from storage; if not by the time DeleteTag returns, the upper bound
//     on reclamation.
type TaggedCache interface {
	// SetTagged writes an entry and associates it with tags. Repeated calls for
	// the same key replace the previous tag set.
	SetTagged(ctx context.Context, key string, value []byte, ttl time.Duration, tags ...string) error

	// DeleteTag deletes every entry associated with the tag.
	DeleteTag(ctx context.Context, tag string) error
}
