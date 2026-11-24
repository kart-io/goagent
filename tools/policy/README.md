# tools/policy: Tool Permission Rules

[English](README.md) | [中文](README_CN.md)

Decides whether a tool call is allowed, denied or sent for approval, based on the tool name and, for calls with a command argument, a command prefix. The policy plugs into a `tools.Pipeline` as a pre-call hook and applies to any tool (coding tools, MCP tools, HTTP tools). Use it when a Dispatcher should run some calls directly, refuse others, and ask a human about the rest.

## Usage

```go
import (
    "github.com/kart-io/goagent/tools"
    "github.com/kart-io/goagent/tools/policy"
)

p := &policy.Policy{
    Rules: []policy.Rule{
        {Tool: "read_file", Decision: policy.Allow},
        {Tool: "run_command", Command: "go test", Decision: policy.Allow},
        {Tool: "run_command", Command: "rm", Decision: policy.Deny},
    },
    Default:      policy.Ask,
    SessionTools: []string{"write_file"},
}

dispatcher, err := tools.NewDispatcher(registry, tools.DispatchConfig{
    Pipeline: &tools.Pipeline{
        Pre:      []tools.PreHook{p.Hook()},
        Approver: approver, // receives PreAsk calls
    },
})
```

`p.Decide(call)` returns the verdict directly, without a Dispatcher.

## Key API

| Identifier | Description |
|---|---|
| `Decision`: `Ask`, `Allow`, `Deny` | Rule verdict; the zero value is `Ask` |
| `Rule{Tool, Command, Decision}` | One rule; `Tool: "*"` matches all tools, an empty `Tool` matches none |
| `Policy` | `Rules`, `Default`, `CommandArg` (default `"command"`), `ArgsArg` (default `"args"`), `SessionTools` |
| `(*Policy).Decide(call)` | Returns the verdict for a call |
| `(*Policy).Hook()` | Adapts the policy to a `tools.PreHook` |
| `CommandGrant`, `ToolGrant` | `tools.SessionGrant` values offered to the approver for "allow for this session" |

## Behavior Notes

- The strictest matching verdict wins (Deny > Ask > Allow), regardless of rule order; `Default` applies only when no rule matches. Adding rules can only tighten the result.
- `Hook` maps `Allow` to `PreAllow`, `Deny` to `PreDeny` with feedback `"<tool> is denied by policy"`, and `Ask` to `PreAsk`.
- A rule with `Command` only matches calls that have the command argument. The command is split on whitespace; argv-style tools also contribute each element of the `ArgsArg` array as one word.
- Allow rules require the same first word and never match commands containing shell control characters, quotes or backslashes. Deny and ask rules compare the file name of the first word (`/usr/bin/git push` matches `"git push"`); for a command they cannot judge, they decide at least `Ask`.
- An allow rule allows every argument of that command. Deny rules can be bypassed (`env rm`, `sh -c`, script files), so do not rely on them for isolation; use a container executor.
- On `Ask`, the session grant offered for a command call is narrowed to the first word, plus the second word when it is a letters-only subcommand (`go test`); commands with shell control characters get no session grant. Tools in `SessionTools` without a command argument get a whole-tool grant; other tools can only be approved once.

## See Also

- [Tool Middleware guide, Dispatcher Pipeline](../../docs/guides/TOOL_MIDDLEWARE.md#dispatcher-pipeline)
- [Harness migration](../../docs/guides/HARNESS_MIGRATION.md)
