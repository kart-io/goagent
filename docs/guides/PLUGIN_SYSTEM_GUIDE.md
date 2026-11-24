# Plugins

[English](PLUGIN_SYSTEM_GUIDE.md) | [中文](PLUGIN_SYSTEM_GUIDE_CN.md)

Agent capability packs are covered in the first section; connecting MCP servers is covered in the last section.

## Agent Capability Packs

A plugin bundles tools, prompt sections, loop hooks, middleware, callbacks, and process-level resources, and is installed into an Agent via `plugin.Install`. `builder` does not depend on `plugin`; goagent-harness preset components can wrap plugins directly.

```go
type Orders struct{ cfg OrdersConfig }

func (*Orders) Name() string    { return "orders" }
func (*Orders) Version() string { return "1.0.0" }
func (o *Orders) Contribute(ctx context.Context, env *plugin.Env) (plugin.Contribution, error) {
    db, err := plugin.Lookup[*sql.DB](env.Resources(), "database.primary")
    if err != nil { return plugin.Contribution{}, err }
    return plugin.Contribution{
        Tools:          []interfaces.Tool{newLookupOrder(db)},
        PromptSections: []builder.PromptSection{{Name: "orders", Text: func(builder.PromptScope) string { return "退款前先查询订单" }}},
    }, nil
}
func (*Orders) Requires() []string { return []string{"database"} }

host, err := plugin.NewHost(database, &Orders{cfg: cfg}, redact)
if err != nil { return err }
if err := host.Start(ctx); err != nil { return err }
defer host.Stop(context.Background())

b := builder.NewSimpleBuilder(client).WithDurableScope("support")
if err := plugin.Install(ctx, b, host, plugin.Dispatch(tools.DispatchConfig{MaxConcurrency: 4})); err != nil { return err }
agent, err := b.Build()
```

Rules (the code is authoritative):

- Plugin names are unique within a Host: non-empty, no leading or trailing whitespace, and no `/`, `@`, or `.` (`.` is the resource namespace separator, and a dot in the name would let the prefix protection be bypassed); versions are non-empty, have no leading or trailing whitespace, and contain no `/`. `NewHost` reports all name, version, duplicate-name, and unregistered-dependency problems at once; only after these pass does it check for dependency cycles.
- `Host.Start` calls `Starter.Start` in dependency topological order (keeping registration order within the same level); if any start fails or the ctx ends, already-started plugins are stopped in reverse order, and `Stop` is not called on the failed plugin itself. A Host cannot be restarted; `Stop` stops plugins in reverse order and merges errors, and repeated calls return nil.
- Resources can only be published by a plugin in its own `Start` via `env.Provide("<plugin name>.<name>", v)`; other plugins retrieve them with `plugin.Lookup[T]`. Resources are cleared after the Host stops, so deactivate the Agent before stopping the Host.
- `Install` requires the Host to be started; it first calls `Contribute` on the selected plugins and validates the results (duplicate tool, prompt section, prompt context, or middleware names; prompt sections or contexts missing a Name; PreSteps missing an Identity; nil hooks), and writes to the Builder only after all checks pass. `Only(names...)` selects plugins; `Dispatch(cfg)` wraps all plugin tools through a single Dispatcher and is required for durable runs.
- A plugin PreStep enters the durable log as `plugin/<name>@<version>/<Identity>`; after a version upgrade, resuming an old run fails with `operation intent or attempt policy changed`. The durable intent combines the identities of all PreSteps on the Agent in registration order (host PreSteps without an identity count as an empty string), so adding, removing, or adjusting plugins with PreSteps, changing the `Only` selection or the registration order, upgrading the version of any plugin with PreSteps (including patch versions), or the host adding or removing PreSteps will all make in-flight durable runs unrecoverable; drain in-flight runs before making these changes.
- The `RequestErrors` of all plugins are merged and registered once, and the recovery count is the maximum of the plugins' `MaxRecoveries`; `Install` overrides the recovery limit previously set by `WithRequestError` (when all selected plugins have 0, the Builder default of 3 is used); to customize the limit, the host must call `WithRequestError` after `Install`.
- A given Builder should be Installed only once.
- `Host` is not reentrant: plugins must not call `Start`/`Stop` of the same Host from within `Start`/`Stop` (this deadlocks). Multiple goroutines can `Install` the same Host concurrently, so plugins' `Contribute` must be concurrency-safe; `Install` must not run concurrently with `Host.Stop`.

See `orchestration/plugin/` in goagent-examples for an example.

## Connecting MCP Servers

Tools from MCP servers do not need to be wrapped as plugins. `mcp/remote` establishes sessions on top of the official SDK, and the `interfaces.Tool` values returned by `remote.Tools` can be passed directly to the Builder:

```go
import (
    "github.com/kart-io/goagent/mcp/remote"
    "github.com/modelcontextprotocol/go-sdk/mcp"
)

session, err := remote.Connect(ctx, &mcp.CommandTransport{Command: exec.Command("./my-mcp-server")})
if err != nil {
    return err
}
defer session.Close()

mcpTools, err := remote.Tools(ctx, session)
if err != nil {
    return err
}
agent, err := builder.NewSimpleBuilder(client).WithTools(mcpTools...).Build()
```

For the HTTP transport and behavior notes, see [Official MCP client adapter](PRIORITY_CAPABILITIES.md#official-mcp-client-adapter).
