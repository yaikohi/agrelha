# Spec: Forge is a loader agrelha can name

## Assumptions

1. The target is the furuncu class of bug, not a general loader refactor: a
   CurseForge pack that really runs **Forge** is recorded as something else, so
   the panel tells the player to build the wrong client.
2. "Forge" means legacy MinecraftForge (`TYPE=FORGE`, `FORGE_VERSION`), a
   distinct loader from NeoForge, not an alias for it.
3. No migration of existing rows. `reconcileInstalledBuild` already runs on
   every list/status pass and will correct furuncu by itself once the reader can
   recognise the manifest.
4. Nobody is asking for a Forge option in the create wizard. Forge arrives only
   as a fact read off a pack, never as an operator choice.

Correct these or I proceed on them.

## Objective

A Minecraft world running Forge shows "Forge 1.20.1" in agrelha, and mod
browsing for it returns Forge files.

Today `furuncu` is stored as `mc_version=26.3, loader=fabric` while the
installed server is Minecraft 1.20.1 + Forge 47.4.10. The cause is not a bug in
one function: CurseForge over-tags that pack (`gameVersions` lists Fabric,
Forge, 1.20.1 and five 26.x versions, with the same `fileId` under
`modLoader` 1 and 4), so `PackFacts` from the index cannot be right. The
ground truth is `.forge-manifest.json`, which the installer writes next to the
world and which `internal/infra/installed` already exists to read.

The domain has no Forge, which is the real gap:
`domain.Loader` is `fabric | neoforge | vanilla`, and `NormalizeLoader` maps
every non-Fabric string to NeoForge.

## Scope — every place this reaches

| File | Change | Why it is in scope |
|---|---|---|
| `internal/domain/pack.go` | `LoaderForge Loader = "forge"`; `ParseLoader` case; `NormalizeLoader` three-way | the type that has no Forge |
| `internal/infra/installed/reader.go` | `.forge-manifest.json` branch, reusing `forgeManifest` | the fix the user asked about |
| `internal/infra/content/curseforge/client.go:37` | `LoaderForge: 1` in `modLoaderType` | otherwise mod search asks CF for NeoForge files for a Forge world |
| `internal/web/pages/manager.go:313` | `case "forge"` in `minecraftLoaderLabel` | **not in the first draft**: an unknown Loader fell through to "Vanilla", so the UI would have called a Forge world unmodded |
| `internal/domain/instance.go:455` | third branch: `TYPE=FORGE`, `FORGE_VERSION=latest` | **blast radius**: only reached by the modlist path, never by a CurseForge pack (`TYPE=AUTO_CURSEFORGE` short-circuits above it). Without it, `NormalizeLoader` guessing NeoForge is a live hazard the moment a Forge value lands on a non-pack world |
| `internal/infra/content/modrinth/client.go:381,416` | nothing — `NormalizeLoader` fix covers it | Modrinth's facet name for Forge is `forge` |

Not in scope: the CurseForge client's `resolvePackFacts` version/loader guessing
(`client.go:293`, `313`). It is wrong for furuncu and cannot be made right —
no CF field distinguishes the pack's real loader from the author's tag soup.
The installed manifest is the answer, which is why this spec reads disk instead.

Not in scope: the wizard's loader radio. Per assumption 4.

Dropped from the first draft: `internal/app/access/mods.go`. Its `SetVersion`
has no caller anywhere, and it writes through the git state store, which is a
nil `Committer` under `RUNTIME=docker`. Adding `FORGE_VERSION` there would be
work on dead code.

Found while implementing: `internal/infra/store/instance_repo.go:105` parses the
stored loader back with `ParseLoader`, which is why the `ParseLoader` case is
load-bearing rather than cosmetic. Without it a row corrected to `forge` reads
back as no Loader at all, and `reconcileInstalledBuild` rewrites it on every
list pass forever.

## Verification

- `task test` green.
- New: `TestReadsTheForgeManifest` in `internal/infra/installed/reader_test.go`,
  shaped like the two beside it, with the real body from furuncu's disk:
  `{"minecraftVersion":"1.20.1","forgeVersion":"47.4.10"}`.
- New: a `NormalizeLoader("forge") == LoaderForge` case, which is the assertion
  that would have caught the whole class.
- New: `instance.go` env case asserting `TYPE=FORGE` for a modlist world.
- On fogo after deploy: `/minecraft` shows furuncu as **Forge 1.20.1**, the
  `instances: corrected the recorded build from what is installed` line appears
  once in `journalctl -u agrelha`, and the container is **not** recreated
  (`docker inspect` uptime unchanged — a restart here would mean env drifted).

## Success criteria

1. furuncu reads Forge 1.20.1 in the UI with no operator action.
2. No existing world changes its recorded loader or its container env.
3. A Forge world's mod browsing returns Forge files, not NeoForge.

## Boundaries

- Always: run `task test` before saying it is done; keep `ParseLoader`
  non-guessing.
- Ask first: anything that would rewrite `mc_version`/`loader` rows in bulk, or
  a converge that recreates a running container.
- Never: commit or push (user does that), add a Co-Authored-By trailer.

## Open question

The user still has to rebuild their *client* as Forge 1.20.1 — this change fixes
what agrelha tells them, not their launcher profile. Worth a line in the UI, or
is the correct loader name enough?
