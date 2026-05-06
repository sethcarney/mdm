# mdm skills audit

Check installed skills for available updates and security advisories.

## Usage

```
mdm skills audit [skills...]
```

For each installed skill sourced from a git remote, mdm queries the skills.sh API to check:

- Whether a newer version is available.
- Whether any security advisories apply (via OSV and the skills.sh advisory database).

Local skills and skills from unknown sources are reported as unchecked.

## Output

```
Project skills:

  my-skill
    ✓  up-to-date
    ✓  no advisories

  outdated-skill
    ▲  update available
    ✓  no advisories

  risky-skill
    ▲  update available
    ✗  1 advisory: prompt-injection risk (high)
       Audited by skills.sh · 2025-03-14

Global skills:

  local-skill
    —  local source, skipped

Audit complete: 3 checked, 1 outdated, 1 advisory
```

### Sync status values

| Status | Meaning |
|---|---|
| `up-to-date` | Skill matches the latest published version |
| `outdated` | A newer version is available |
| `unknown` | Could not determine status (no hash stored or API error) |
| `local` | Installed from a local path, not a git remote |
| `unchecked` | Not a recognised source type |

### Advisory severity levels

| Level | Meaning |
|---|---|
| `high` | Significant risk — review before use |
| `medium` | Moderate concern |
| `low` | Minor issue |

## Flags

| Flag | Description |
|---|---|
| `--global, -g` | Audit global skills only |
| `--project, -p` | Audit project skills only |
| `--json` | Output as JSON |

## Examples

```bash
# Audit all installed skills
mdm skills audit

# Audit only global skills
mdm skills audit -g

# Audit a specific skill
mdm skills audit my-skill

# Machine-readable output
mdm skills audit --json
```

## JSON output

With `--json`, each entry includes the skill name, scope, source, sync status, and any audit results:

```json
[
  {
    "name": "my-skill",
    "scope": "project",
    "source": "owner/repo",
    "syncStatus": "up-to-date",
    "audits": [
      {
        "provider": "skills.sh",
        "status": "pass",
        "summary": "No known advisories"
      }
    ]
  }
]
```
