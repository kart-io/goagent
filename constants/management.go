// Management service defaults.
package constants

import "time"

// Defaults for required management.Limits values: 0 in the matching field
// selects these values.
const (
	DefaultManagementPageSize           = 50
	DefaultManagementWorkers            = 4
	DefaultManagementPollInterval       = 20 * time.Millisecond
	DefaultManagementOutboxPollInterval = 100 * time.Millisecond
	DefaultManagementPersistTimeout     = 5 * time.Second
	DefaultManagementDeliveryBackoffMax = 256 * time.Second
	DefaultManagementBuildRetryDelay    = time.Second
)

// DefaultManagementMaxRequestBytes is the inbound request size cap of the
// httpapi, a2a and mcpserver adapters when MaxInputBytes is 0. It is an abuse
// guard for untrusted clients, so 0 does not mean unlimited there.
const DefaultManagementMaxRequestBytes int64 = 1 << 20
