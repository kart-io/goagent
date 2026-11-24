# General-Purpose Skills Loading

[English](SKILLS.md) | [中文](SKILLS_CN.md)

`github.com/kart-io/goagent/skills` is a read-only loading package in the main repository that can be used on its own.
It does not depend on the Builder, models, workflows, plugins, or the Harness, and the execution kernel does not depend on it.
The optional `github.com/kart-io/goagent/tools/skill` wraps a Catalog as the `load_skill` tool.

## Basic Usage

```go
root, err := os.OpenRoot("/srv/approved-skills")
if err != nil { return err }
defer root.Close()

catalog, err := skills.Discover(ctx, root)
if err != nil { return err }
defer catalog.Close()

metadata := catalog.List()                         // name, description, source, version, digest
loaded, err := catalog.Load(ctx, "faq")             // body, resource file names
if err != nil { return err }
data, err := catalog.ReadReference(ctx, "faq", "references/refunds.md")
if err != nil { return err }
// The caller consumes metadata, loaded.Body, and data.
```

To parse in-memory files on their own, use `skills.Parse(data)`. Runnable example:

```sh
env -u GOROOT go run ./examples/skills/03-catalog
```

It demonstrates the [three loading levels: metadata, full instructions, and supporting resources](https://agentskills.io/integrate-skills), and needs no model key.
Parsing follows the [Agent Skills format](https://agentskills.io/specification) and supports UTF-8 BOM, LF, CRLF,
full-line frontmatter delimiters, and the standard optional fields; name, description, and compatibility are strictly validated.
Names are limited to the ASCII lowercase letters, digits, and single hyphens supported by the original Harness, and the directory name must match the name.
YAML uses GoAgent's `codec/yaml.DecodeMapping`, which checks size before expanding aliases and preserves field types;
all mapping keys and the values of `metadata` must be strings, numeric versions should be written as `version: "1"`, and an explicit null does not stand in for omitting an optional field.
Small aliases are allowed; duplicate keys (including alias keys), cyclic aliases, expansion over the limit, the YAML merge key `<<`, and the binary scalar `!!binary` are rejected.
Frontmatter is text metadata; binary material should go in supporting files, so that YAML-to-JSON conversion does not change the bytes.
Scientific-notation exponents in numbers also count toward the decoding budget, so short numeric text cannot trigger huge rational conversions.
After passing the YAML type and budget checks above, unknown extension fields are ignored; `disable-model-invocation` is kept as an extension field for the host to interpret.

## Sources and Ownership

- Discover only scans `<name>/SKILL.md` in the directory handles passed in; it does not choose default directories or interpret `~`.
- The caller decides the source order. Multiple directories are processed in argument order and the first one with a given name wins; within a single directory, skills are sorted by name.
- Directories without a SKILL.md are skipped. A malformed, name-mismatched, or unreadable skill fails the whole discovery, including skills that would be overridden.
- Source directories are only borrowed until Discover returns; the caller is responsible for closing them. The Catalog holds its own subdirectory handle for each selected skill, cleans them up on failure, and on success the caller Closes it.
- List returns independent copies. Close can be called repeatedly; afterwards List still shows metadata, but Load/ReadReference return `fs.ErrClosed`. Unknown skills support `errors.Is(err, fs.ErrNotExist)`.
- Source is the SKILL.md path at discovery time and is only for tracing; it must not be used to reopen the file instead of the Catalog's restricted reads.

Metadata and body are snapshots taken at discovery time; the resource list and resource contents are read on demand. `Version` comes from the author-declared
`metadata.version`, and `Digest` is the SHA-256 of the full raw SKILL.md, including frontmatter, BOM, and line-ending differences.
The digest does not cover resource files and does not represent a whole-package signature or an immutable version. When whole-package consistency is needed, the host should provide an immutable directory or verify resources separately;
to update the body, Discover again and close the old Catalog after its existing users finish.

## In-Memory Sources, Subsets, and Custom Limits

- `skills.DiscoverFS(ctx, fsys, source, opts)` discovers `<name>/SKILL.md` from a generic `fs.FS` (such as `embed.FS` or `os.Root.FS()`);
  `source` is only used for `Metadata.Source`. Whether symbolic links are followed is decided by `fsys`: `embed.FS` has no symbolic links, `os.Root.FS()` is confined to the root directory,
  and `os.DirFS` sets no boundary, which the caller is responsible for before passing it in. Paths still go through the same slash-separated relative path checks; `DiscoverFS` does not hold handles, and `fsys` belongs to the caller.
- `skills.DiscoverWithOptions(ctx, opts, roots...)` behaves the same as `Discover`, but lets you adjust the anti-abuse limits in `DiscoverOptions`:
  `MaxFileBytes` (a single SKILL.md and referenced files), `MaxTotalBytes` (total SKILL.md bytes read in one discovery, unlimited by default),
  `MaxFiles` (number of entries), `MaxDescription` (description characters), `MaxDepth` (resource directory depth). 0 keeps the default, `skills.Unlimited` (-1) means no limit, and other negative values are an error.
- `catalog.Select(names...)` returns a sub-catalog containing only the selected skills (unknown names are an error); the sub-catalog's `Load`/`ReadReference` cannot reach other skills.
  The sub-catalog shares the underlying handles with the parent catalog; only the parent's `Close` releases them, the sub-catalog's `Close` is a no-op, and after the parent is closed the sub-catalog also returns `fs.ErrClosed`.
- `catalog.Prompt()` outputs `name: description` lines sorted by name, skipping skills with `DisableModelInvocation`, and returns an empty string for an empty catalog, for the host to inject into the system prompt.

## File Boundaries

Skill directories are held as handles with `os.Root`; renaming a directory or replacing the original path does not redirect reads to the new directory.
These directory-identity semantics depend on the platform's `os.Root` implementation; Go's JS and Plan 9 implementations do not track directory renames.
This run was verified on Linux; Windows/macOS were only cross-compiled, and their runtime behavior was not verified.
Resource paths are slash-separated relative paths; `./references/a.md`, `references/./a.md`, and repeated slashes are accepted.
Before normalization, any `..` component, absolute path, backslash, or empty path is rejected, and file arguments do not accept a trailing `/` or `/.`.
Internal file links are readable and out-of-bounds links are rejected; discovery does not follow skill directory links, and resource enumeration does not recursively follow directory links.
Only regular files can be read; a FIFO does not wait for a writer. A restricted root directory is not a sandbox: existing hard links and mounts inside the directory, and source authorization, are managed by the host.

Resource limits live in `constants`: SKILL.md at most 1 MiB, the conservatively estimated JSON size of the expanded frontmatter at most 1 MiB,
a single referenced file at most 4 MiB, the discovery scan and each resource enumeration
at most 10000 entries each, at most 2000 selected skills, and resource enumeration depth at most 32 levels. Exceeding a limit returns an error rather than silently truncating.
Each selected skill holds a subdirectory handle for its lifetime, so actual capacity is also bounded by the process handle limit; during updates, old and new Catalogs coexisting also use extra resources.
Load also enumerates resources, and when enumeration fails it returns an error as a whole rather than returning the cached body alone.
Operations that take a context check for cancellation between scan steps and before and after reading files; lock waits and single system calls on ordinary local files are not guaranteed to be interrupted immediately, and Close waits for in-flight reads to finish.

## Tool and Host Responsibilities

```go
tool := skilltool.New(catalog)
agent, err := builder.NewSimpleBuilder(model).
    WithSystemPrompt(promptSelectedByHost).
    WithTools(tool).
    Build()
```

`load_skill` accepts `name` and an optional `file`. When file is omitted it returns `name/instructions/files`; when file is given it returns
`name/file/content`. When there are no supporting files, `files` is the empty array `[]`. The public Catalog returns raw bytes, while the tool's content
only outputs UTF-8 text that can be encoded losslessly, and non-UTF-8 content is explicitly rejected; binary material should be read directly by the host.
The tool does not own the Catalog, does not inject prompts automatically, does not interpret authorization, and does not execute scripts.

The tool distinguishes business rejections from execution failures: unknown skills, missing files, and recognized invalid path/permission/size-limit errors return `Rejected` with a reason.
Recognized resource enumeration entry-count or depth limit violations while loading the body also return `Rejected`, and the full instructions and file list are not delivered partially.
Unclassified I/O failures return a non-nil execution error that keeps its `errors.Is` identity, so that transient failures are not recorded as completed rejections.
Go's `os.Root` does not expose identifiers for some boundary errors; these errors also go into the execution error chain unchanged, the read is still blocked, and classification is not guessed from the error text.

The direct `WithTools(tool)` example above is for ordinary calls. A durable Builder should use tools assembled through the Dispatcher and a stable DurableScope;
when using the Harness, the host also needs to pin the Manifest explicitly and manage the Assembly lifecycle. Completed successes or rejections are replayed under the original call identity
and are not re-read because the files changed later; to read the latest resources, issue a new call.

The host decides which sources/skills are available to callers, the source override order, how the catalog is displayed, whether to reload after context compaction,
and whether to provide the tools a skill declares. `AllowedTools` does not grant authorization automatically; `DisableModelInvocation` does not automatically filter the public Catalog.
Hiding catalog hints is not a substitute for authorization; if a user must not read a skill, block access at the source selection or tool-call policy layer.
Git/OCI downloads, installation updates, and script execution continue to use their own source management or execution environments.

The Harness `preset/skills.go` keeps the configuration entry point, default directories, workspace path interpretation, trust grants, prompts, and Manifest assembly,
and reuses this Catalog and tool. The component version is bumped to 3, and the Manifest switched from a body digest to a digest of the full raw SKILL.md,
so changing only metadata also makes a pinned Manifest mismatch; durable runs from the old version need the host to handle version migration.

v3 also tightens the old Harness parsing rules: booleans use `true/false` (`yes/no` are no longer accepted), purely numeric names need quotes,
newly added metadata reads require values such as `metadata.version` to be strings and no longer ignore their types; name, description, and compatibility are limited
to 64, 1024, and 500 characters respectively. Invalid documents, including overridden skills, all make Build fail; fix the corresponding files before migrating.

These public APIs have been released since GoAgent v0.52.0; the Harness depends on a GoAgent version that includes them and is verified with `GOWORK=off`.
