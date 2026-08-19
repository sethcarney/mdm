# Experimental features

Some mdm features ship behind an experimental gate while the standards or
conventions they build on are still settling. Experimental features:

- may change or be removed in **any** release — they are exempt from semantic
  versioning until they graduate
- are hidden from `--help` and shell completion until enabled
- print a warning banner on every invocation while enabled
- never destroy state used by stable features — mdm's lock and state files
  preserve keys they don't recognize, so an experimental section cannot be
  dropped by a binary that doesn't know it

## Enabling a feature

Persistently, per user:

```bash
mdm experimental enable <feature>
```

Or for a single invocation / CI, via the environment (comma-separated feature
names, or `all`):

```bash
MDM_EXPERIMENTAL=<feature> mdm <command>
```

The environment variable always wins: it activates a feature even if it was
disabled with `mdm experimental disable`.

## Commands

```
mdm experimental
├── list                 # show features, status, and spec links
├── enable <feature>     # persist an opt-in (stored in the global lock file)
└── disable <feature>    # remove the persisted opt-in
```

## Current experimental features

This release ships none. `knowledge` and `plugins` graduated to full
support in v2 — the command groups are always visible and need no opt-in.
`mdm migrate` clears any stale persisted opt-ins for them.

## Graduated features

| Feature | Graduated | Now |
|---|---|---|
| `knowledge` | v2 | [`mdm knowledge`](specs/knowledge.md), fully supported |
| `plugins` | v2 | [`mdm plugins`](specs/plugins.md), fully supported |
| `plugins` | Manage Agent Plugins — portable packages of skills and MCP servers; install, validate, and wire MCP servers into agent configs. See [docs/specs/plugins.md](specs/plugins.md). | [Agent Plugins v1.0.0](https://agent-plugins.org) |
