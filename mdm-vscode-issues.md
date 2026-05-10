# mdm-vscode GitHub Issues

Create each of these as an issue in `sethcarney/mdm-vscode`.

---

## Issue 1

**Title:** `checkCliAndWarn`: unhandled promise rejection silently swallows CLI errors

**Labels:** bug

**Body:**

In `src/extension.ts:708`, the outer promise chain has no `.catch()`:

```typescript
void client.checkInstalled().then((installed) => {
  if (!installed) {
    void vscode.window.showErrorMessage(...).then((action) => {
      if (action === "Configure Path") {
        void vscode.commands.executeCommand(...);
      }
    });
  }
});
```

Any rejection from `checkInstalled()` (e.g. an unexpected `execFile` failure) is silently swallowed by `void`. The user sees nothing and the extension continues as if the CLI check succeeded.

**Fix:**

```typescript
void client.checkInstalled().then((installed) => {
  if (!installed) { ... }
}).catch(() => {
  // CLI check failed — optionally surface a warning
});
```

---

## Issue 2

**Title:** `findSkillInteractive`: async search callback can mutate a disposed QuickPick

**Labels:** bug

**Body:**

In `src/extension.ts:785–808`, the debounced search fires an async closure that updates `qp.items` and `qp.busy`. If the user accepts or hides the picker while that search is in-flight, `settle()` runs first — calling `qp.dispose()` — and the async then resumes and mutates the now-disposed object.

The `query !== qp.value.trim()` guard at line 788 partially mitigates this, but doesn't cover all disposal timing, and `qp.busy = false` in the `finally` block has no guard at all.

**Fix:** Add a `settled` check before any QuickPick mutation inside the async callback:

```typescript
const found = await client.findSkills(query);
if (settled || query !== qp.value.trim()) return;
qp.items = [...];
```

```typescript
} finally {
  if (!settled && query === qp.value.trim()) {
    qp.busy = false;
  }
}
```

---

## Issue 3

**Title:** `installSkillWithRetry`: retry conditions rely on fragile CLI output string matching

**Labels:** bug

**Body:**

In `src/extension.ts:869` and `881`, the retry logic keys off exact substrings in CLI output:

```typescript
if (output.includes("audit-blocked")) { ... }
if (output.includes("allow-hidden-chars")) { ... }
```

This creates a hidden, undocumented contract between the extension and the CLI. If the mdm CLI ever changes these error strings, the retry logic silently breaks — the user just sees a generic failure instead of the expected "install anyway?" prompt.

**Recommendation:** Define these strings as a stable interface — either by outputting structured JSON errors from the CLI (with a machine-readable `code` field), or by documenting them explicitly in both repos with a cross-reference comment so they're not changed inadvertently.

---

## Issue 4

**Title:** Tree providers: `EventEmitter` instances and `_refreshTimer` are never disposed

**Labels:** bug

**Body:**

In `src/mdmTreeProvider.ts`, `_onDidChangeTreeData` EventEmitters are created as class fields but are never registered with `context.subscriptions` and are never explicitly disposed. Additionally, `_refreshTimer` (a pending `setTimeout`) is never cleared when the extension deactivates — `deactivate()` in `extension.ts` is an empty no-op.

This means:
- The timer can fire after extension unload
- EventEmitters accumulate if the extension is reactivated

**Fix:** Implement `dispose()` on both tree providers:

```typescript
dispose(): void {
  if (this._refreshTimer !== undefined) {
    clearTimeout(this._refreshTimer);
  }
  this._onDidChangeTreeData.dispose();
}
```

Then register in `activate()`:

```typescript
context.subscriptions.push(skillsProvider, agentsProvider);
```

---

## Issue 5

**Title:** `mdmClient`: JSON from CLI cast without runtime schema validation

**Labels:** bug

**Body:**

Multiple call sites in `src/mdmClient.ts` parse CLI stdout and immediately cast via TypeScript's `as`:

```typescript
return JSON.parse(text) as KnownAgent[];   // line 170
return JSON.parse(text) as FindSkillResult[]; // line 238
return JSON.parse(text) as AuditResult[];  // line 257
return JSON.parse(text) as RulesEntry[];   // line 305
```

TypeScript `as` casts are erased at runtime — they provide zero safety. If the CLI version mismatches the extension's type expectations, or if output is truncated/malformed, the result is cryptic `Cannot read properties of undefined` errors rather than a useful diagnostic.

**Recommendation:** Add at minimum a structural shape check before casting (e.g. verify the result is an array), or adopt a lightweight schema library like `zod` for the CLI response types. This makes version mismatches diagnosable instead of mysterious runtime crashes.

---

## Issue 6

**Title:** Inconsistent scope typing: agents use `boolean`, skills use `MdmScope`

**Labels:** enhancement

**Body:**

All skill operations use the `MdmScope = "global" | "project"` union type, but agent methods use a raw `boolean`:

```typescript
// skills — consistent
async addSkill(repo: string, scope: MdmScope, ...): Promise<void>

// agents — inconsistent
async removeAgent(name: string, global: boolean): Promise<void>
async addAgent(name: string, global: boolean): Promise<void>
```

A reader has to mentally translate `true → "global"` and `false → "project"` when working across the two. This also means call sites that hold a `MdmScope` value must do an ad-hoc conversion before calling agent methods.

**Fix:** Update `removeAgent` and `addAgent` to accept `MdmScope` instead of `boolean`, and update call sites accordingly.
