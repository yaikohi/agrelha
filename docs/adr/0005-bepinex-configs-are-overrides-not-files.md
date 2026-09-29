# BepInEx Configs Are Stored as Overrides, Not as Files

agrelha stores only the Settings an operator deliberately changed, never a
mod's whole configuration file. The file on the World's disk stays the
authority on what Settings exist; git is the record of what was decided about
them.

## Context

Every Valheim mod writes its own `.cfg` into `BepInEx/config` the first time it
loads. BepInEx's `ConfigFile.Save()` emits a consistent, machine-readable shape
for each entry — a `##` description, then `# Setting type:`, `# Default value:`
and optionally `# Acceptable values:` or `# Acceptable value range:`, then
`Key = value`. That metadata is enough to build a typed editor, which is the
reason this was worth doing at all.

Three facts constrain where those values can live.

**The files are large.** One World's config directory is ~651 KB across 21
files. `Therzie.Warfare.cfg` alone is 207 KB and declares 1,398 Settings;
`Therzie.Wizardry.cfg` is 202 KB. A ConfigMap caps at 1 MiB. Committing whole
files would sit at roughly two thirds of a hard limit that fails at apply time,
and would put 200 KB blobs in every git diff.

**The mod owns the file, not agrelha.** BepInEx rewrites these files on its own
schedule — on version changes, and whenever a mod adds or removes a Setting. A
copy in git goes stale silently, and a stale copy written back over the live one
would revert Settings the operator never touched.

**agrelha cannot see the files directly.** They live on a node-local PVC, and
agrelha has no `pods/exec` in the `valheim` namespace — a deliberate restriction
recorded in `agrelha-rbac.yaml`.

There was also a working example of the alternative. An earlier iteration copied
whole files from a ConfigMap into the config directory. It wrote them one
directory below where BepInEx reads, so for a month every committed value was
inert, and nothing reported a problem. Whole-file copying is not merely
expensive; it fails quietly.

## Decision

**A Generated config is discovered; an Override set is stored.**

- A sidecar copies the mod-written `.cfg` files to the shared backups export,
  which agrelha already mounts read-only. Same mechanism as the existing
  `build-watch` sidecar: no new RBAC, no exec, no new image.
- agrelha parses those files to learn what Settings exist, their types, defaults
  and accepted values.
- git holds one ConfigMap key per config file, containing only the keys the
  operator changed, written in BepInEx's own syntax.
- At pod start, an init container merges the Override set into the Generated
  config before the server loads.

**The merge is a textual substitution, not a re-render.** Only the right-hand
side of an overridden `Key = value` line changes. The file header, every
description and metadata comment, blank lines, section order, and any line the
parser did not understand all pass through byte-identical. r2modman, the
reference implementation for this format, re-emits files from its own model and
silently drops the `## Settings file was created by plugin …` header on every
save. Substituting in place makes that class of bug unrepresentable.

**An Override whose key is absent is appended as a bare `Key = value`.** That is
exactly what BepInEx writes for an orphaned entry, and BepInEx preserves such
entries across its own saves — so an Override for a Setting a mod has dropped
survives rather than evaporating.

## Consequences

- **Storage is trivial.** An Override set for a heavily tuned mod is a few
  kilobytes. The 1 MiB ceiling stops being a consideration.
- **Diffs say what changed.** A one-setting edit is a one-line diff, against a
  file whose every line is a decision somebody made.
- **agrelha can answer "what did I change?"** — it has both the mod's default
  and the current value. r2modman cannot: it never parses `# Default value:`.
- **Removing an Override restores nothing.** The Generated config keeps its
  current value until something writes over it. "Reset to default" therefore
  *writes* the default as an Override, and "forget" is a separate action whose
  confirmation says the server keeps its value. This is the single most
  counter-intuitive consequence and the easiest to implement wrongly.
- **A fresh PVC converges on the second boot.** With no Generated config to
  merge into, the merge writes the bare Override set; BepInEx reads it, then
  rewrites the file with full metadata on its first save, keeping the values.
- **agrelha's own image runs on the game pod.** The merge is Go, not shell,
  because quoting a 207 KB file with arbitrary values through `sh` is how
  configs get corrupted. That costs an imagePullSecret in the `valheim`
  namespace.
- **Settings the mod never described stay free text.** 298 of Warfare's 1,398
  entries are bare `key = value` with no metadata; there is nothing to infer a
  type from, and inventing one would be a guess presented as a fact.

## Alternatives considered

**Commit whole config files.** The obvious approach, and what the previous
implementation did. Rejected on size (651 KB against a 1 MiB cap), on diff
quality, and most of all because it makes git the authority on what Settings
exist — a claim it cannot keep true across a mod update.

**Give agrelha `pods/exec` and read the files live.** Simplest to build, and
removes the sidecar entirely. Rejected: it reverses a deliberate RBAC decision,
and exec into a game pod is a much larger capability than "read some text
files".

**Have the pod write its configs back to a ConfigMap itself.** Keeps everything
in the Kubernetes API with no shared filesystem. Rejected: it needs write RBAC
for the game pod, and 651 KB of configs runs straight back into the 1 MiB limit.

**Merge with `sed`/`awk` in the existing busybox init container.** No new image,
no pull secret. Rejected: this is r2modman's mistake in a worse language, on
files where a quoting bug corrupts a World's configuration silently.

**Re-render the file from the parsed model on save.** Conceptually cleaner than
line surgery. Rejected: it is precisely how r2modman loses the file header, and
it would turn every save into a whole-file diff.
