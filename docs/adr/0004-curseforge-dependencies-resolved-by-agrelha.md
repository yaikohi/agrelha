# CurseForge Dependencies Are Resolved by agrelha, Modrinth's by the Server

The two Providers are handled asymmetrically on purpose. agrelha resolves a
CurseForge mod's dependencies itself and writes them into the Mod list;
Modrinth's it may leave to the server image. The asymmetry is forced by
upstream, not preference.

## Context

`itzg/minecraft-server` installs individual mods through two environment
variables, one per Provider: `MODRINTH_PROJECTS` and `CURSEFORGE_FILES`. They
look symmetric and they are not.

For Modrinth, the image resolves a project's required dependencies and fetches
them. For CurseForge it cannot, and says so plainly in its own documentation:

> The files processing can detect if a dependency is missing from the given
> list, but is not able to resolve the dependencies otherwise since their
> metadata only gives the mod ID and not the specific file version/ID that is
> needed.

CurseForge's dependency records name a *mod*, never a *file*. Choosing a file
means asking which release of that mod matches this world's Minecraft version
and Loader — a question only something holding the world's configuration can
answer. The image has the API key and the mod id; it does not have a rule for
picking among twenty builds.

So a CurseForge mod with dependencies, added naively, produces a server that
downloads the mod, notices the gap at startup, and fails. Per
[ADR 0003](0003-mod-install-failure-is-fatal.md) that is the worst possible
place for the failure: the operator has left, nothing is wrong yet on the
page they were looking at, and the World is now in a crash loop.

## Decision

**agrelha resolves CurseForge dependencies at install time.** When a mod is
added, agrelha walks its required dependencies, picks for each the newest file
matching the Instance's Minecraft version and Loader, and writes every one of
them into `curseforge.txt` as a pinned `slug:fileId` entry.

Three consequences follow deliberately:

1. **Every CurseForge entry is pinned.** A bare slug would let CurseForge pick
   the newest file at boot, which is a different answer on different days and
   makes an export a guess rather than a record. This matches how Valheim mods
   are pinned.
2. **Dependencies are written down, not implied.** They appear in the Mod list
   as ordinary entries. An operator reading the file sees everything the World
   will run, and a Restricted dependency is caught at install rather than
   discovered at boot.
3. **Modrinth is left alone.** agrelha already resolves Modrinth dependencies
   for its own purposes, and the image resolves them again; the redundancy is
   harmless and removing it is not this decision's business.

## Consequences

- The asymmetry is permanent until CurseForge's API returns file ids in
  dependency records. It will look like an inconsistency to anyone reading the
  two code paths side by side. That is what this record is for: **removing the
  CurseForge resolution to make the providers symmetric reintroduces boot-time
  crash loops**, and the symmetry it appears to buy is an illusion.
- agrelha now needs a CurseForge API key of its own to resolve anything, since
  CurseForge has no anonymous API. Without one, CurseForge is absent rather than
  broken — search returns nothing and says why.
- Install becomes slower for CurseForge than for Modrinth: one API call per
  dependency, at the moment the operator clicks. That is the right place to
  spend the time.
- A dependency graph agrelha resolved may drift from what a newer file of the
  parent mod requires. Updating a pinned CurseForge mod must re-resolve its
  dependencies, exactly as the Valheim mod-update path re-resolves Thunderstore
  trees.

## Alternatives considered

**Let the image fail and report the missing dependency.** Cheapest, and it is
what the image is built to do. Rejected: it moves a solvable problem to the one
place ADR 0003 says problems must never surface, and the message a player sees
is a stopped server.

**Require the operator to add dependencies by hand.** Honest, and it keeps
agrelha out of the business of choosing files. Rejected: the operator has strictly
less information than agrelha does — they would be reading the same API, by hand,
to answer a question agrelha already has the Instance's Loader and version to
answer.

**Write bare slugs and let CurseForge choose newest at boot.** Removes the file
question entirely. Rejected: it makes the running mod set unknowable and unreproducible,
and a World that boots differently on Tuesday is the failure mode this project
has already been bitten by twice.
