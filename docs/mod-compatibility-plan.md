# Mod-combination compatibility checking (Minecraft)

Status: **designed, not implemented.** Grilled 2026-09-11.

A mod list where every mod is individually valid can still kill the server on
boot. This is the gate that catches those combinations before an Instance is
created.

## What exists today

| Thing | Where | Reality |
|---|---|---|
| `CheckCartCompatibility` | `app/content/compat.go`, called from `web/handlers/wizard` | **Advisory only.** Returns fit counts + `bestLoader` for display. Nothing blocks |
| `modpackindex.CheckLoader/AnalyzeLoader/BestLoader` | `infra/content/modpackindex/loaderfit.go` | Loader fit for *packs*, used in wiring |
| `ResolveRequiredDependencies` | `infra/content/modrinth` | Wired only into `InstallMod` (adding to a live instance), **not** into creation |

**Two fields are already fetched on every project and never read:**

- `domain.ModProject.ServerSide` — `"required" \| "optional" \| "unsupported"`.
  A client-only mod on a dedicated server is one of the commonest instant
  crashes.
- `domain.VersionDependency.DependencyType` — already carries `"incompatible"`.
  Authors declare conflicts explicitly and agrelha ignores them.

`CheckCartCompatibility` also checks project-level `Loaders` but **not**
`GameVersions` against the chosen Minecraft version.

So the effective gate today is: loader fit, advisory, wizard-time.

## Decisions

1. **Static metadata first; smoke boot is an opt-in toggle, not the default.**
   Static costs ~1–2s and no cluster resources. A boot test competes for the
   same RAM the budget already caps with `MAX_RUNNING` — and it cannot run at
   all when the cluster is full, which is exactly when someone is creating an
   Instance.
2. **Tiered verdicts, with override.** Block on certainly-fatal-and-declared;
   warn on the rest. The operator can override even a block, and the override is
   recorded in the audit log. Modrinth metadata is author-maintained and often
   stale; a gate with no bypass turns one bad `server_side` tag into "agrelha
   refuses to build my server", and the operator routes around the tool by hand.
3. **Mod lists only, never Packs.** A published Pack is internally consistent and
   its author booted it. Re-validating produces false positives — packs
   legitimately ship client-side mods the server install omits — and it
   contradicts the CONTEXT.md rule that a Pack owns its Loader and version.
   CurseForge packs cannot be statically analysed at all without an API key.

## Phase 1 — static gate

Runs on the wizard's review step and again at `CreateInstance` (the wizard is not
the only caller; the gate belongs in the application layer, not the handler).

### Blocking

| # | Check | Why fatal |
|---|---|---|
| 1 | `ServerSide == "unsupported"` | Client-only mod on a dedicated server |
| 2 | Instance Loader not in the mod's `Loaders` | Wrong loader, will not load |
| 3 | Instance `MCVersion` not in the mod's `GameVersions` | No compatible build exists |
| 4 | A `required` dependency resolves to nothing for (mcVersion, loader) | Missing hard dependency |
| 5 | Mod A declares mod B `incompatible` and both are selected | Author-declared conflict |

### Warning

| # | Check | Note |
|---|---|---|
| 6 | `ServerSide == "optional"` | Loads but may be inert server-side |
| 7 | An `optional` dependency is absent | Reduced functionality |
| 8 | No exact `GameVersions` match but a nearby patch exists (1.21 vs 1.21.1) | Usually fine, sometimes not |
| 9 | Loader fit < 100% (today's `CheckCartCompatibility`) | Feeds the `bestLoader` suggestion |

### What static checking cannot catch

State this in the UI, because it sets the expectation the smoke test exists to
meet: mixin conflicts, two mods shading different versions of the same library,
Java version mismatches, memory exhaustion, worldgen/datapack collisions. Every
one of these passes metadata validation and dies on boot.

## Phase 2 — optional smoke test

An explicit toggle on the wizard's review step: *"Boot-test this mod set before
creating"*.

- Renders the Instance spec into a **Job**, not a Deployment: small tier heap,
  `emptyDir` instead of a PVC, offline mode, short world, hard timeout.
- **Pass** = the server reaches `Done (x.xxx s)! For help, type "help"` inside
  the timeout. **Fail** = crash, exception, or timeout. Capture the last N log
  lines either way and show them.
- Tear down the Job and its volume afterwards regardless of outcome.

### Wizard draft state

The smoke test takes minutes, so the wizard must survive the operator leaving the
page. This is the real cost of the feature and the reason it is phased:

- A **draft** row in SQLite keyed by a draft id, holding the wizard's field
  state plus the smoke-test status and result. Drafts are ephemeral working
  state, not desired state, so they belong in the store and **never** in git.
- The wizard resumes from a draft id; the Hub shows a badge for drafts awaiting
  a result.
- Push the result over the existing Datastar SSE stream.
- Drafts need a TTL and a prune, or they accumulate forever.

## Open decisions

Deliberately unresolved; settle before implementing phase 2.

1. **Does a smoke test create an Instance row?** Recommendation: **no** — keep it
   an ephemeral Job keyed to the draft. CONTEXT.md defines Lifecycle
   `provisioning` as "what the operator has asked an Instance to be", and a
   boot test is not yet that request. Creating a real Instance to test it makes
   Lifecycle lie.
2. **Does a smoke test consume the budget?** Recommendation: **yes** — it is a
   real server process and must pass the same admission check, or it will get
   OOM-killed at exactly the moment the cluster is busy.
3. **Draft TTL and prune policy.**
4. **Do warnings need acknowledging**, or only blocks?
5. **Should Mod update detection extend to Minecraft?** Not built, and the
   reason for excluding it has changed — record which reason is load-bearing
   now. The original argument was cost: Valheim update checks were free because
   Thunderstore's bulk index sat warm in memory, while Modrinth would need one
   HTTP call per mod. That argument is dead. The bulk index turned out to be
   hours stale (it reported four mods up to date that r2modman could see had
   newer versions, and omitted a fifth entirely), so Valheim now asks about each
   mod by name too. Both games would cost the same.

   What survives is a modelling argument, and it only covers part of the field.
   A Minecraft Instance with Source `modpack` has its versions chosen by the
   Pack, so a per-mod update is the wrong operation — CONTEXT.md already says a
   Pack owns its Instance's Loader and Minecraft version. But an Instance with
   Source `modlist` has no such owner, and per-mod updates are exactly right for
   it. That is the case to build if this is revisited: it needs Modrinth version
   resolution filtered by Minecraft version and Loader, which Thunderstore does
   not require.

## Out of scope

- Valheim. Thunderstore metadata is far weaker than Modrinth's — no
  `server_side`, no structured incompatibility — so the same design does not
  transfer. Revisit separately. (Valheim does have Mod update detection, which
  is a different feature: it reports that a newer version exists, never that it
  is safe. Flagging a risky update belongs here, in this plan.)
- CurseForge. Restricted packs and no API key; see the memory note.
- Continuous re-validation after creation (a mod updating and breaking a running
  server). Different problem, different trigger.
