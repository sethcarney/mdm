# Experimental features

Some mdm features ship behind an experimental gate while the standards or
conventions they build on are still settling. Experimental features:

- may change or be removed in **any** release — they are exempt from semantic
  versioning until they graduate
- are hidden from `--help` and shell completion until enabled
- print a warning banner on every invocation while enabled
- never touch state used by stable features (e.g. they use their own lock
  files), so enabling one cannot corrupt a stable workflow

## Enabling a feature

Persistently, per user:

```bash
mdm experimental enable knowledge
```

Or for a single invocation / CI, via the environment (comma-separated feature
names, or `all`):

```bash
MDM_EXPERIMENTAL=knowledge mdm knowledge list
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

| Feature | What it does | Spec |
|---|---|---|
| `knowledge` | Manage Open Knowledge Format (OKF) bundles — install, validate, and audit markdown knowledge bases for AI agents. See [docs/specs/knowledge.md](specs/knowledge.md). | [OKF v0.1](https://github.com/GoogleCloudPlatform/knowledge-catalog/tree/main/okf) |
