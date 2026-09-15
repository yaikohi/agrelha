# Agrelha

Single-operator control plane for the dedicated game servers on the `yaya`
cluster. Declarative state (mods, configs, access) is committed to git and
reconciled by ArgoCD; imperative actions (start/stop/restart, logs) go straight
to the Kubernetes API.

This file is the *domain* language only. Structural vocabulary — Layer, Port,
Adapter, Plane — is defined in [docs/architecture.md](docs/architecture.md).

## Language

### Servers

**Instance**:
One Minecraft server, with its own Deployment, PVC, Service and config. Identified
by a stable Number and a human Name.
_Avoid_: Slot, world, server (when you mean the Minecraft one specifically)

**Slot**:
Retired, and now removed from the code. It meant "which world is active" back
when all Minecraft worlds shared a single Deployment. Two traces remain on
purpose: `SlotName()` (an identifier sanitiser, poorly named but harmless) and
the `-slot` suffix on an Instance's config ConfigMap object, kept because
renaming it would recreate the object and restart every Instance.
_Avoid_: using this word at all

**World**:
The save data an Instance generates and plays on, stored at `/data/<LEVEL>`. One
Instance owns exactly one World.
_Avoid_: Save, level (except when naming itzg's `LEVEL` variable), map

**Duplicate**:
Creating a new Instance from an existing one, either by copying its World or by
regenerating from the same Seed. The supported way to change what a World runs,
since that is a new Instance by definition.
_Avoid_: clone, fork, copy (as a noun)

**Worlds** (player-facing only):
The word the public dashboard uses for Instances, because that is what players
call them. A deliberate UI synonym — never a separate concept, and never used in
code or in operator-facing pages.

**Terrain type**:
The world-generation algorithm (default, flat, amplified, large biomes), passed
to itzg as `LEVEL_TYPE`. Not a kind of World.
_Avoid_: WorldType, level type

### Content

**Loader**:
The mod engine a server runs: NeoForge or Fabric for Minecraft, BepInEx for
Valheim. A property of the Instance, not of where its mods came from. Minecraft
names its Loader in the UI because there is a choice; Valheim's is implied by
Modded, because BepInEx is the only one.
_Avoid_: type, engine, modloader

**Source**:
How an Instance's content is defined: `modpack`, `modlist`, or `vanilla`. Orthogonal
to Loader — a modpack has a Loader too.
_Avoid_: type, mode, install method

**Vanilla**:
An Instance running with no Loader at all. On Valheim that means BepInEx is not
installed, which is the only way Steam achievements remain earnable — so Vanilla
is a promise to players, not merely an empty mod set. A Vanilla Instance's mod
set is empty and cannot change.
_Avoid_: unmodded, clean, stock

**Modded**:
An Instance running a Loader, whether or not any mods are installed yet. A Valheim
world with BepInEx and zero mods is Modded: achievements are already off. Modded
and Vanilla describe the Loader, never the size of the Mod list.
_Avoid_: has mods, using mods

**Pack**:
A published, versioned collection of mods with its own configs, identified by a
Provider and a Ref. When an Instance has one, the Pack — not the operator —
decides its Loader and Minecraft version.
_Avoid_: modpack (as a distinct concept), bundle

**Provider**:
Who distributes a Pack: CurseForge or Modrinth. A distributor, never a Loader and
never a kind of server.
_Avoid_: platform, host, source (that word means something else here)

**Mod list**:
An operator-chosen set of individual mods resolved from Modrinth, used when Source
is `modlist`. The operator picks the Loader and Minecraft version.
_Avoid_: modpack, custom pack

**Mod update**:
A newer version of an already-installed mod exists upstream. Applying one repins
the Mod list to that version, re-resolves the mod's dependencies, and restarts
the World; it never changes the game itself. Only pinned entries can have one:
an entry with no version recorded has nothing to compare against.
_Avoid_: update (unqualified), upgrade, patch

**Restore point**:
The Mod list an Instance had immediately before a Mod update, kept so the update
can be undone. One per Instance, and only valid while the Instance's Mod list is
still exactly what that update wrote — any other change to the list withdraws it,
because reverting would discard that change too.
_Avoid_: backup (that word means the World save), snapshot, rollback

**Server update**:
New game binaries, from the upstream image or the game's own store. Unrelated to
the Mod list, and never triggered by applying a Mod update. The UI names which
kind it means, because the two are the same word and different consequences.
_Avoid_: update (unqualified), game update, version bump

**Tier**:
An Instance's memory allocation: small (4 GiB), medium (8 GiB), large (12 GiB).
Tiers exist so concurrent Instances fit a fixed RAM budget.
_Avoid_: size, plan

### State

**Lifecycle**:
What the operator has asked an Instance to be: `running`, `stopped`,
`provisioning`, or `error`. A desired state — it says nothing about whether
players can connect yet.
_Avoid_: status, state (unqualified)

**Availability**:
Whether players can actually connect right now, derived from pod readiness. An
Instance can be Lifecycle `running` and still unavailable while it installs mods
or generates its World.
_Avoid_: online status, up, state (unqualified)

**Online / Offline**:
The player-facing rendering of Availability. The only state words that appear in
the UI.
_Avoid_: Up, Down, Active

**Health**:
What is known to have gone wrong with an Instance, independent of what the
operator asked for (Lifecycle) and of whether players can connect right now
(Availability). An Instance can be Lifecycle `running`, Availability offline, and
carry a last Incident explaining why — three independent facts, each worth
showing.
_Avoid_: status, state (unqualified), error

**Incident**:
One recorded failure of an Instance: when it happened, how it ended (exit code,
out of memory, crash loop, a mod that could not be installed) and the log tail
captured at the moment of detection. A failure that starts the server and a
failure that prevents it starting are both Incidents.
_Avoid_: crash (that is one kind of Incident), error, event

Lifecycle, Availability and Health are never compared to each other and never
share a field. Kubernetes' own `Phase` ("Running", "Pending") belongs to neither — it is
an input to Availability, and its capital-R "Running" must never be confused with
Lifecycle's lowercase `running`.

### Access

**Admission**:
Who is allowed to join a server. Valheim controls this with a shared password,
Minecraft with a whitelist.
_Avoid_: access (that is the page, not the concept), auth

**Operator**:
Who is allowed to administer a server in-game. Valheim calls them admins
(`ADMINLIST_IDS`, Steam64), Minecraft calls them ops (`ops.txt`, username).
_Avoid_: admin (ambiguous with the agrelha operator), owner

**Access** (page name only):
The page where Admission and Operators are managed for a game. Not a domain
concept — it names a screen, and covers whichever of the two the game exposes.

**Player**:
Someone who joins a game server. Distinct from the single human who runs agrelha.
_Avoid_: user, member

**Operator (agrelha)**:
The single authenticated human who administers agrelha itself. Where ambiguity is
possible, say "the agrelha operator" for this and "Operator" for the in-game role.
_Avoid_: admin, owner

## Rules

**A Pack owns its Instance's Loader and Minecraft version.**
When Source is `modpack`, those are facts read from the Pack, not settings. Any
screen offering to change them is offering to break the Instance.

**A Seed is immutable once a World exists.**
The Seed only influences generation. Changing it afterwards changes nothing about
the existing World, so presenting it as editable states something untrue.

**Changing what an Instance runs means creating a new Instance.**
Swapping the Pack or Loader under a played World is not an edit; it is a different
server. The old World stays on its own PVC. Turning a Vanilla Instance Modded is
exactly this change — it revokes achievements that were already earned against
the promise Vanilla made — so it is offered as Duplicate, never as an edit.

**A Vanilla Instance never gains mods.**
The rule is enforced in the domain, not only in the UI: an install refused on the
page must also be refused when the API is called directly. Duplicate is the
supported path to a Modded copy.

**Lifecycle is never compared with Availability**, and neither is compared with
Kubernetes' `Phase`.
