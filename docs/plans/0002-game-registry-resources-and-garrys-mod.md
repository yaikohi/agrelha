# Plan: game registry, per-instance resources, and Garry's Mod

Agreed 2026-10-04. **Supersedes `0001-per-instance-resources-and-tier-presets.md`** —
that work is absorbed here as slice 2, because it rewrites the same
`domain.Instance` branches this generalisation does.

## Context

The goal is a third game: Garry's Mod, for the server a friend assembles every
year. A survey of what that actually costs found the problem is not Garry's Mod.

**`ports.Game` is ceremonial.** Six of its nine methods have zero production
callers — `ID`, `Display`, `Providers`, `ResolveContent`, `RuntimeSpec`,
`AdmissionModel`, `OperatorIDKind`. `ports.ContentProvider` and
`ports.ModResolver` have no implementations at all. Writing a third `Game` is a
formality.

**The real cost is ~40 hardcoded two-branch sites**, concentrated in
`domain.Instance` (17 of them), every one shaped `if GameID == GameValheim {…}
else {…}` — so Minecraft is not chosen, it is merely what is left. A Garry's Mod
instance today would silently get a `mc-<slug>-NN` deployment, `TYPE=NEOFORGE`
and `MODRINTH_PROJECTS` in its environment, **JVM heap settings**, Minecraft tier
sizing, `mc-` backup filenames, and rows written into `mc_instances`.

**Three of those collide with the access work just shipped:**

| site | effect on a third game |
|---|---|
| `domain/account.go:35` | `ParseInstanceRole` whitelists two games → GMod roles unparseable → **non-admin grants never match** |
| `wiring.go:677` | `requests.WithCreate` two-branch → **an approved GMod request creates a Minecraft world** |
| `web/guard` `Mounts()` | no rows → **GMod routes get no instance authorization at all** |

**One abstraction already fits.** `domain.Pack` + `SourceModpack` means "an
upstream-owned collection the server resolves itself" — exactly a Steam Workshop
collection. It is merely hard-gated to Minecraft in the schema and repo mapper.
`PackageCatalog`, the interface nominally offered for content, assumes
`namespace/name` identity, semver and a dependency tree, and fits Workshop badly.

## The model

A **registry**: one data entry per game, replacing the branches. Per-game
knowledge splits three ways, because the architecture ratchet (`domain` imports
nothing internal) and editable tiers force it:

| kind | examples | lives in |
|---|---|---|
| Identity / shape | name prefix, table, display, capabilities, defaults | `domain` |
| Settings | tiers, budgets, ceilings, creation limit — runtime-editable | `app/capacity` (SQLite) |
| Adapters | `SpecRenderer`, `Runtime`, namespace, k8s client | `wiring` |

### Decisions

| | |
|---|---|
| Access to identity facts | `GameProfile` **passed as a parameter** to `domain.Instance` methods |
| Identity | New value object `domain.InstanceID{GameID, Number}` |
| Game-specific state | Moves off `Instance` into a per-game value object |
| Storage | Per-game tables kept; registry declares table + `toRow`/`fromRow` mapper; core columns mapped once |
| Unknown `GameID` | **Hard error.** Never a default game |
| Dead code | Delete the six unused `Game` methods, `ContentProvider`, `ModResolver` |
| Capabilities | Explicit flags, started minimal and extended as differences bite |
| Handlers | Generic handler for GMod first; Valheim and Minecraft converge later |
| Dashboard | Generalised from two hardcoded cards to a list |

### Why parameter-passing

Moving the methods out of `domain` into a service would be the **anemic domain
model** — `Instance` becomes a DTO and its behaviour moves to a procedure. The
purest alternative, holding the profile inside the aggregate, is infeasible:
there are **310 `Instance{}` literals, 299 in tests, and no constructor**, so the
invariant could not actually be enforced in Go. Passing the profile keeps
behaviour on the entity with no hidden global, and the compiler finds every call
site.

### Vocabulary (CONTEXT.md)

**Game profile** — the fixed shape of a game: how its resources are named, which
table holds its Instances, what it is capable of. Not its settings.

**Capability** — something a game supports (mods, configs, backups, player
count). Drives which tabs, routes and guard mounts exist.

**Instance id** — the identity of an Instance: a game and a number together.
Numbers are only unique within a game.

---

## Slice 1 — registry, identity, repo. No behaviour change.

The risk is front-loaded here, where the existing suite can catch it.

| # | What |
|---|---|
| 1 | `domain.GameProfile`, `domain.Capabilities`, `domain.InstanceID` |
| 2 | Per-game value object; move `Loader`/`Pack`/`MCVersion`/`Difficulty`/`Gamemode`/`WorldType`/`Password`/`Seed` off `Instance` |
| 3 | Thread `GameProfile` through `domain.Instance`'s 17 branching methods; delete every `if GameID == GameValheim` |
| 4 | Registry in `app/games`; adapters assembled in wiring; `docs/architecture.md` rows |
| 5 | Registry-driven repo: table + mapper pair, core columns generic |
| 6 | Fix the three access collisions: `ParseInstanceRole`, `requests.WithCreate`, `guard.Mounts()` |
| 7 | Delete the six dead `Game` methods and the two unimplemented ports |
| 8 | Generalise the dashboard, nav and SSE signals from two hardcoded games to a list |

**The whole of slice 1 must be behaviour-preserving.** Valheim and Minecraft
worlds keep their names, env, paths, tables and backup filenames exactly.

## Slice 2 — per-instance resources (absorbs plan 0001)

A **Tier is a preset, not a binding**: it seeds an Instance's resources at
creation and does nothing after. An Instance owns concrete resources and may
diverge; the tier reference is kept as provenance so drift is visible.

| # | What |
|---|---|
| 9 | `domain.Resources` (memory + CPU, request + limit); per-game tier catalogues in SQLite |
| 10 | **Backfill migration**: every existing Instance gets concrete resources from today's hardcoded tier values |
| 11 | Render resources from the Instance, not the tier; heap-init becomes a Minecraft-only tier field |
| 12 | Budget sums concrete resources; per-game ceiling enforced in `app/capacity`, admin bypasses |
| 13 | `FreeCreations` becomes a setting (global total across games, default 1) |
| 14 | `/overview`: all worlds, all games, inline tier and resource editing, per-game settings |
| 15 | Opt-in per-world "apply tier" for drifted worlds. Never bulk, never automatic |

**Step 10 is the one that must not go wrong.** Anything other than an exact
backfill silently resizes running worlds on rollout.

## Slice 3 — Garry's Mod

| # | What |
|---|---|
| 16 | `gmod_instances` table (with pack columns), per-game VO, registry entry |
| 17 | Ungate `Pack`/`SourceModpack` from Minecraft; add `ProviderSteamWorkshop` |
| 18 | Manifest template set; community image pinned by digest |
| 19 | Steam Web API adapter: read-only collection listing + last-updated for drift |
| 20 | Generic instance handler, used by GMod only for now |
| 21 | Collection-drift banner reusing the pending-restart UI |
| 22 | Tiers 2 / 4 / 8 GiB; backups cover config and `data/` only |

GMod specifics: a world is a **collection ID** plus gamemode and map, all
first-class fields. Admission is `sv_password`. **No** operator management, **no**
GSLT, **no** A2S player count for now — GMod worlds report unknown occupancy and
need Force to restart, which is the honest fallback the restart queue already
has. One shared Steam Web API key in OpenBao under `agrelha`, used both by
agrelha for listing and injected into every GMod world.

**Backups must exclude downloaded Workshop addons.** They can be many gigabytes
and all of it re-downloads from the collection ID.

---

## Verification

**Slice 1 — the whole point is that nothing changes**

1. Full suite green with no test expectations altered. Any test needing a changed
   expectation means behaviour moved; investigate rather than update it.
2. Render a Valheim and a Minecraft deployment before and after → **byte-identical
   manifests**.
3. An Instance with an unregistered `GameID` produces a hard error, never a
   Minecraft-shaped anything.
4. `guard`'s coverage ratchet still passes; every protected route classified.
5. Dashboard shows exactly the registered games, driven by the list.

**Slice 2**

6. Deploy with no settings changed → every world keeps its exact memory request
   and limit, nothing restarts. This is the backfill check.
7. A 16 GiB Valheim tier and a 9 GiB Minecraft tier each render correctly; Valheim
   tiers offer no heap field.
8. Edit a world's resources → shown as drifted; change its tier → **the drifted
   world does not move**; per-world apply restarts only that world.
9. Non-admin blocked above the ceiling, admin not.
10. CPU requests visible in `kubectl describe node game-01`.

**Slice 3**

11. A GMod world starts, loads the collection, and is joinable with the password.
12. Its deployment is named `gmod-*`, its rows are in `gmod_instances`, and its
    env contains no JVM or Minecraft variables. **This is the regression test for
    the entire premise of slice 1.**
13. A non-admin granted the GMod world can operate it — proving
    `ParseInstanceRole` handles three games.
14. An approved GMod creation request creates a *GMod* world, not a Minecraft one.
15. Editing the Steam collection raises the drift banner; restarting clears it.
16. A backup of a GMod world is small and contains no Workshop addons.

## Out of scope

Unifying the instance tables (per-game tables kept deliberately, avoiding a data
migration). Converging Valheim and Minecraft onto the generic handler — deferred
until GMod proves it. A2S player counts. GMod operator management and GSLT.
Modelling Workshop addons individually. A fourth game, though the point of all
this is that it should be nearly free.

## A note on capability churn

Capability flags drive guard mounts, and starting minimal means the flag set will
move while three games migrate — normally how authorization holes appear. This is
covered by `web/guard`'s coverage ratchet, which fails the build on any protected
route that is not explicitly classified. It caught thirteen unguarded routes when
world creation was opened up. It is load-bearing here, not incidental.
