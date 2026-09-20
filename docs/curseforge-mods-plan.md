# CurseForge as a source of individual mods

Design agreed 2026-09-19 in a `/grill-with-docs` session. Not implemented.

Companion decision: [ADR 0004](adr/0004-curseforge-dependencies-resolved-by-agrelha.md),
which records why the two Providers are handled asymmetrically. Read it before
touching the resolution path.

## What is actually missing

CurseForge **Packs already work.** `minecraft-modded/instance-01` (Fluxweave)
and `instance-02` (Wrath of Herobrine) run `TYPE=AUTO_CURSEFORGE` with
`CF_PAGE_URL` today, and no `CF_API_KEY` is configured anywhere — the itzg image
ships its own for Java 17+.

What is missing is **individual mods**. `domain.Instance.Env()` hardcodes the
Mod list to one Provider:

```go
env["MODRINTH_PROJECTS"] = "@/config-mods/mods.txt"     // instance.go:279
```

`instance-03` (bob) is the only `Source: modlist` world today, so it is the only
one this changes.

**The API key is for agrelha, not the servers.** CurseForge has no anonymous
API, so agrelha cannot search or resolve anything without a key. The servers
keep using the image's built-in key for downloads.

> **Accepted trade-off.** Leaving the servers on the image's shared key was a
> deliberate choice, not an oversight. If that key is ever revoked or
> rate-limited, the failure appears at server boot rather than in agrelha.
> Setting `CF_API_KEY` per instance from the same secret is a one-line remedy if
> it bites.

## Storage: a second key, not a new format

`mods.txt` stays exactly as it is — Modrinth, `slug:version`. CurseForge gets its
own key in the same ConfigMap, in CurseForge's own syntax:

```
data:
  mods.txt: |            # MODRINTH_PROJECTS=@/config-mods/mods.txt
    terralith:2.6.4
  curseforge.txt: |      # CURSEFORGE_FILES=@/config-mods/curseforge.txt
    jei:4593548
```

Each file is already precisely what its environment variable expects, so nothing
translates between formats and nothing can mistranslate. Existing instances are
untouched: a world with no CurseForge mods simply has no second key, and
`CURSEFORGE_FILES` is not set.

**Every CurseForge entry is pinned** as `slug:fileId`. A bare slug lets
CurseForge pick the newest file at boot, which makes the running mod set
unknowable and an export a guess.

Files to change:

- `internal/domain/instance.go` — `Env()` sets `CURSEFORGE_FILES` when the
  Instance has CurseForge entries. Keep it off entirely when it has none.
- `internal/infra/manifests/templates/mods.yaml.tmpl` — emit the second data key
  conditionally; it currently hardcodes `mods.txt` alone.
- `internal/infra/manifests/manifests.go` — pass the second body through.
- `internal/app/instances/manager.go` — `GetInstalledMods`, `InstallMod` and
  `RemoveMod` currently assume one list. They need to read and write both, and
  route each entry by Provider.

## The CurseForge client

New adapter `internal/infra/content/curseforge/`, alongside `modrinth/` and
mirroring its shape. It satisfies `ports.ModResolver` and `ports.ContentProvider`
(`internal/ports/game.go`), so nothing in `app` or `web` learns a vendor name.

It needs, at minimum:

- `Search(ctx, query, mcVersion, loader, limit)` — filtered server-side by game
  version and `modLoaderType`, since an unfiltered CurseForge search returns
  mods that cannot run on the world asking.
- `GetMod(ctx, slugOrID)` — metadata, **including `allowModDistribution`**.
- `LatestFile(ctx, modID, mcVersion, loader)` — returns the file id to pin.
- `ResolveRequiredDependencies(ctx, modID, mcVersion, loader)` — see ADR 0004.

Auth is the `x-api-key` header on every call. Config knob `CURSEFORGE_API`
(default `https://api.curseforge.com/v1`) beside `ModrinthAPI` in
`internal/platform/config/config.go`, and `CURSEFORGE_API_KEY` as a secret.

**No key means no CurseForge, not a broken agrelha.** When the key is empty,
wiring leaves the client nil, search returns no CurseForge results and says
"CurseForge not configured", and Modrinth is unaffected — the same shape as
`state/unconfigured` when the git token is missing. Do not fail startup.

## Dependency resolution

Read [ADR 0004](adr/0004-curseforge-dependencies-resolved-by-agrelha.md). In
short: itzg resolves Modrinth dependencies but **cannot** resolve CurseForge's,
because CurseForge dependency records name a mod id and never a file id. agrelha
must resolve them at install and write each one into `curseforge.txt` as a
pinned entry, or any CurseForge mod with dependencies crash-loops at boot.

The Minecraft path already has the seam: `instances.WithDependencyResolver` is
wired to `d.MR.ResolveRequiredDependencies` at `internal/wiring/wiring.go:139`.
It becomes Provider-aware rather than Modrinth-only.

## Restricted mods

A mod with `allowModDistribution: false` cannot be downloaded by anyone but
CurseForge's own client. **No API key changes this** — you already lost
Ducktopia Farlands to the Pack version of it.

Two places, both required:

1. **Search results mark them** and disable Install, so the operator never wants
   something they cannot have.
2. **Install refuses** with a message naming the restriction — the API is the
   authority, and a search result may be stale.

`CONTEXT.md` now defines **Restricted mod** for exactly this.

## Search across both catalogues

One search box. Results from Modrinth and CurseForge merged, each badged with
its Provider, Install routing to the right file. The operator's question is "is
there a mod that does X", not "is there a Modrinth mod that does X".

`internal/web/handlers/minecraft/mods_search.go` holds the existing single-provider
search and is where this lands. Keep the badge visually distinct from the
existing mod-update badge — `CONTEXT.md` separates those concepts and the UI
should not blur them.

## Export

`.mrpack` files carry arbitrary download URLs, and CurseForge hands one over
whenever `allowModDistribution` is true. `internal/app/modpack/mrpack.go` already
writes a `[MODS NOT FOUND ON MODRINTH (CURSEFORGE EXCLUSIVE OR MANUAL)]` report
section; CurseForge mods currently land there and the player gets a modpack
missing mods the server runs.

Embed them instead, with their CDN URL. Only genuinely Restricted mods fall
through to the report — nobody can put those in a modpack.

## The secret, in the right order

Adding a `remoteRef.property` to an ExternalSecret whose key is not yet in
OpenBao makes ESO fail to build the **whole** Secret, and agrelha stops starting
with `CreateContainerConfigError`. This is the media-pod failure already
documented in the homelab notes. So:

1. **First**, seed `apps/agrelha` in `tal 02-platform-config/apps-secrets.tf`
   (`vault_kv_secret_v2 "agrelha"`) with the CurseForge key, and `tofu apply`.
2. **Then** add the property to `manifests/agrelha-secrets.yaml`, which feeds
   `agrelha-env` via `envFrom.secretRef`.
3. Verify with `scripts/check-externalsecret-drift.sh` before committing.

Never step 2 first.

## Out of scope

**Update detection for Minecraft mod lists.** Pinned entries go stale exactly
as Valheim's did, and `docs/mod-compatibility-plan.md` already records Minecraft
modlists as the case to build if revisited. The `modupdates` Checker's `Catalog`
and `Instances` interfaces are narrow enough to extend later. Bundling it would
double this change while the CurseForge client is still unproven.

**Better CurseForge Pack browsing** in the wizard. Packs work; pasting a URL is
merely unpleasant.

## Verification

1. **Modrinth is untouched.** `instance-03` renders byte-identical `mods.yaml`
   before and after, and no `CURSEFORGE_FILES` appears in its Deployment.
2. **A CurseForge mod installs.** Add one with no dependencies; `curseforge.txt`
   gains a pinned `slug:fileId`; the server boots with the mod present.
3. **Dependencies are written down.** Add a mod that has required dependencies;
   every one appears in `curseforge.txt` as its own pinned entry *before* the
   server starts. This is the ADR 0004 behaviour and the one most likely to be
   quietly broken later — assert it in a unit test against a faked catalogue,
   not only by hand.
4. **A Restricted mod is refused.** It is marked in search, Install is
   unavailable, and calling the install endpoint directly still refuses. A
   guard that only exists in the UI is not a guard.
5. **Missing key degrades.** Unset `CURSEFORGE_API_KEY` locally: agrelha starts,
   Valheim and Modrinth work fully, CurseForge search says it is not configured.
6. **Export contains the mod.** A world with a CurseForge mod exports a
   `.mrpack` that actually contains it; a world with a Restricted one still
   lists that mod in the report section.
7. **Mixed world.** One world holding both Modrinth and CurseForge mods boots
   with all of them — the case the whole design exists for.
