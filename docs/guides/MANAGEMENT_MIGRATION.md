# Moving the Unified Management Layer into Core

[English](MANAGEMENT_MIGRATION.md) | [中文](MANAGEMENT_MIGRATION_CN.md)

The general-purpose management code has moved from contrib into the GoAgent main module. As confirmed by users, the switch is direct, with no forwarding packages or aliases kept for the old packages. Business data tables, task states, config version bindings, and HTTP routes do not change because of the move in source ownership.

## Package and Test Ownership

| Original entry point | Current entry point |
| --- | --- |
| `github.com/kart-io/goagent-contrib/management` | `github.com/kart-io/goagent/management` |
| `.../goagent-contrib/management/adapters` | `.../goagent/management/adapters` |
| `.../goagent-contrib/management/httpapi` | `.../goagent/management/httpapi` |
| `.../goagent-contrib/management/pgstore` | Kept; provides the database implementation |
| `.../goagent-contrib/management/harness` | Kept as an optional standalone module |

The unit tests and OpenAPI of the original general-purpose packages moved into core along with the implementation. Database integration tests and the refund host remain in contrib and consume the new management entry points. The core main module adds no pgx, contrib, or Harness dependency; the import layering check forbids execution foundation layers from importing management in reverse.

## Errors and JSON Entry Points

The old `management.ErrX` is replaced directly by `errors.ErrManagementX`, where X is NotFound, Conflict, Invalid, Unauthenticated, Forbidden, Unsupported, Unavailable, ApprovalRequired, Busy, or Owner. The old error entry points are not kept. Use the standard library errors.Is to check fixed sentinel identity; structured classification is provided uniformly by the core errors package.

RawMessage in management entities uses `codec/json.RawMessage`. Encoding and decoding use `codec/json` and no longer depend directly on encoding/json; idempotency comparison keeps exact numbers and rejection of duplicate keys. HTTP requests use the core Strict decoding rules: field names must match the protocol, and unknown fields and invalid UTF-8 are rejected. The business input/result/config fields of responses are still kept in full.

## Cross-Repository Verification of Unreleased Code

GoAgent includes the management packages starting with v0.51.0; v0.50.0 and earlier do not, so changing only the import path does not mean an old released version supports the new packages.

When testing unreleased changes in core together with contrib, use contrib's existing temporary workspace entry point; do not modify the user's go.work, and do not write a local replace into core's go.mod:

```bash
# In the contrib root directory; GOAGENT_CORE_ROOT points to a GoAgent worktree that contains the new management packages.
export GOAGENT_CORE_ROOT=/path/to/goagent
export GOAGENT_PGSTORE_DSN='postgres://postgres:management-local@127.0.0.1:55441/postgres?sslmode=disable'
bash scripts/with-local-modules.sh go -C management test -race -count=1 ./...
bash scripts/with-local-modules.sh go -C management run ./examples/host -migrate -verify -listen 127.0.0.1:0
```

The database must be a dedicated verification instance; do not point explicit migration tables or fault injection at a business database. The example uses an offline model and writes refund records to the database, without making real payment requests.
