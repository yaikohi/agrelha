# Plan: per-instance resources, tier presets, and a worlds overview

Agreed 2026-10-04. Groundwork for adding a third game (Garry's Mod), which is
planned separately.

## Context

Resource tiers are three hardcoded Go constants in `internal/domain/tier.go`,
shared by every game:

| tier | request | limit | heap-init |
|---|---|---|---|
| small | 4 GiB | 6 | 3 |
| medium | 8 GiB | 10 | 6 |
| large | 12 GiB | 16 | 10 |

Three problems, all of which get worse with a third game:

1. **Tiers are global.** A 16 GiB Valheim and a 9 GiB Minecraft cannot both be
   expressed without giving every game both.
2. **`HeapInitMemoryGiB` is a JVM concept in a shared type.** It is meaningless
   for Valheim today and for Garry's Mod tomorrow.
3. **Nothing is editable.** Changing any of it is a code change and a release.

Budgets are already per-game (`VALHEIM_TOTAL_BUDGET_GIB`, `MC_TOTAL_BUDGET_GIB`
and friends), currently unset and running on defaults: Minecraft 24 GiB / 4 / 2,
Valheim 16 GiB / 4 / 2. `game-01` has ~60 GiB allocatable and 16 CPUs, none of
which is reserved by anything today. The 40 GiB ceiling is self-imposed.

## The model

The important shift: **a tier is a preset, not a binding.**

- A **Tier** is a named set of resources belonging to one game. It seeds an
  Instance's resources at creation and does nothing afterwards.
- An **Instance owns its resources** and may diverge from the tier that seeded
  it. The tier reference is kept as provenance so drift is visible.
- A **ceiling** per game caps what a non-admin may choose. The agrelha operator
  is not bound by it.
- Editing a tier **retrofits nothing**. Worlds whose resources no longer match
  their tier are marked as drifted, with an opt-in per-world re-apply.

This is why SQLite is the right home for the catalogue: losing it costs
convenience, not truth. Every running world keeps its own concrete resources,
which are rendered into the git manifests exactly as today.

### Vocabulary

`CONTEXT.md` must be updated — **Tier** no longer means a guaranteed size.

**Tier** — a named resource preset belonging to one game, applied when an
Instance is created. Not a contract: an Instance may diverge afterwards.
_Avoid_: size, plan, class

**Resources** — the memory and CPU an Instance actually requests and is limited
to. Owned by the Instance, rendered into its manifest.

**Ceiling** — the largest Resources a non-admin Account may choose for a game.
The agrelha operator may exceed it.

**Drift** — an Instance whose Resources no longer match the Tier that seeded it.

## Decisions

| | |
|---|---|
| Tier catalogue | Per game, stored in SQLite, UI-editable |
| Instance resources | Concrete, per instance, may diverge from any tier |
| Spec contents | Memory request + limit, CPU request + limit |
| Minecraft heap-init | Moves out of the shared type into a Minecraft-only tier field |
| Ceiling | Per-game max instance size, alongside the existing per-game budget |
| Creation limit | **Global total** across all games, superadmin-settable (today 1) |
| Non-admin sizing | May choose at creation **and** resize later, within the ceiling |
| Tier edits | Never retrofit; opt-in re-apply per world |
| Game settings | Move from env into SQLite, seeded from env on first boot |
| Overview | One admin-only page: all worlds, all games, editable |

### Accepted trade: the creation limit is global

With a global limit of 1 and three games, someone holding a Valheim world files
a Request for a Garry's Mod server rather than creating it directly. That is the
approval flow behaving correctly, and the limit is now a setting rather than the
`const FreeCreations = 1` it is today — raise it to 2 and the annual GMod server
needs no approval.

## Migration — the part that must not go wrong

Existing Instances carry a tier string and no concrete resources. The migration
**must backfill each Instance's resources from the current hardcoded values for
its tier**, so that deploying this changes no running world's size.

Anything else silently resizes live worlds on rollout. Verification step 1 exists
solely to catch this.

## Commit sequence

| # | What |
|---|---|
| 1 | `CONTEXT.md` vocabulary + ADR for tier-as-preset |
| 2 | `domain.Resources` (mem request/limit, cpu request/limit), `domain.Tier` |
| 3 | `tiers` + `game_settings` tables; instance resource columns; **backfill migration** |
| 4 | `ports.Tiers`, `ports.GameSettings` + store adapters |
| 5 | `internal/app/capacity`: catalogue CRUD, ceiling checks, budget accounting, drift — plus `docs/architecture.md` row |
| 6 | Render resources from the Instance, not the tier, in both manifest templates |
| 7 | Move heap-init into the Minecraft game package as a Minecraft-only tier field |
| 8 | Budget accounting sums concrete resources instead of tier lookups |
| 9 | `FreeCreations` becomes a setting read from `game_settings` |
| 10 | `/overview` page: worlds table, inline tier/resource edit, per-game settings, tier catalogue |
| 11 | Wizard and instance settings honour the ceiling for non-admins |

Commits 1–4 are additive and independently safe. Commit 6 is the one that changes
what gets rendered; it must land after the backfill in 3.

### Notes on specific commits

**6 — rendering.** Today the template asks the tier for sizes. It must ask the
Instance. Both `infra/manifests/valheim` and the Minecraft equivalent need this,
and `manifests_test.go` should gain an assertion that a rendered deployment
carries the Instance's resources rather than any tier's.

**7 — heap-init.** Keep it an explicit field rather than deriving it from the
limit. The current mapping (6→3, 10→6, 16→10) follows no clean formula, so any
derived rule would silently change behaviour. It becomes an optional field shown
only when editing a Minecraft tier.

**11 — ceiling enforcement.** Enforce in `app/capacity`, not in the handler, so
the wizard, the instance settings form and any future API all go through one
check. Admin bypasses it there, consistently.

## Verification

1. **Deploy with no settings changed → every existing world keeps its exact
   current memory request and limit, and nothing restarts.** This is the
   migration check; if any world resizes, the backfill is wrong.
2. Define a Valheim tier at 16 GiB; create a world on it; the rendered manifest
   requests 16 GiB and the world starts on `game-01`.
3. Define a Minecraft tier at 9 GiB with a heap-init; confirm the JVM flag in the
   rendered deployment matches, and that Valheim tiers offer no heap field.
4. Edit a world's resources directly, away from its tier → overview shows it as
   drifted against its provenance tier.
5. Change that tier's definition → **the drifted world does not change**; the
   per-world re-apply action is offered and only restarts that world when used.
6. As a non-admin, try to create a world above the game's ceiling → refused. As
   admin, the same size succeeds.
7. As a non-admin, resize an owned world upward within the ceiling → allowed, and
   told it restarts. Above the ceiling → refused.
8. Set the global creation limit to 2 → a second world no longer files a Request.
9. Budget: sum of concrete resources is what the dashboard reports, and creating
   a world that would exceed the per-game budget is refused with the real numbers.
10. CPU requests appear on the rendered deployments and `kubectl describe node
    game-01` shows them reserved.

## Out of scope

Garry's Mod itself — separate design, though this exists to make it clean.
Vertical autoscaling or any automatic resizing. Per-account RAM allowances
(rejected in favour of per-game ceilings plus a global creation limit). Disk and
network limits. Awareness of `game-01`'s physical capacity: budgets remain
operator-set numbers, not derived from the node.
