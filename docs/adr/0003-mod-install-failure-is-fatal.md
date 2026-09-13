# A World Does Not Start With the Wrong Mod Set

When a declared mod cannot be installed, the server does not start. Failures are
classified so that a transient fault is retried and a permanent one is reported,
and the failure is made visible in agrelha rather than only in `kubectl`.

## Context

Valheim instance #02 sat in `Init:CrashLoopBackOff` for five restarts. Three
things had gone wrong, and only one of them was the mod's fault:

1. The mod-reconciler parsed `Namespace-Name` and `namespace/name/version`, but
   not `Namespace-Name-Version` — which is the identifier Thunderstore's own copy
   button produces, and the form 14 of the 18 entries used. It asked the API for
   a package named `BowPlugin-1.8.7` and got a 404.
2. `unzip` exits 1 for warnings as well as errors. One mod packed on Windows
   warned about backslash path separators, and under `set -eu` that aborted the
   whole script.
3. **agrelha did not notice.** `PodStatus` iterated `ContainerStatuses` only, so
   an init container failing five times reported `restarts: 0`, produced no
   Incident and no event. The crash detection built for exactly this purpose was
   blind to the case where the server never starts at all.

The first two were bugs and are fixed. The third exposed a design question the
codebase had never answered: **what should happen when a mod cannot be
installed?**

The same question had already been answered accidentally elsewhere. Minecraft
instance #03 crash-looped ten times because `shedaniel-clothconfig` and `cloth`
were not real Modrinth slugs; itzg's image hard-fails on an unresolvable project.
Both runtimes were failing hard, neither deliberately.

Both games also accepted the bad entry in the first place: `InstallMod` resolves
a version to pin it, and on failure logged a warning and installed the mod
anyway. A typo at install became a crash loop at boot.

## Decision

1. **A World does not start with a mod set that differs from what was declared.**
   Valheim mods are client-side as well as server-side: a player imports the
   exported profile and joins. A server quietly missing a plugin is a mismatch,
   not a degraded experience. Starting anyway trades a visible failure for an
   invisible one.

2. **Classify the failure; do not treat every error alike.** `curl -f` collapses
   every HTTP error into exit 22, which makes a deleted package
   indistinguishable from an outage. The reconciler captures the status instead:
   `404`/`410` are permanent and fail immediately; `5xx` and connection failures
   are transient and retried a bounded number of times within the same container
   run.

   Kubernetes already retries a failed init container, but its backoff grows to
   five minutes — the wrong instrument for a two-second blip.

3. **`unzip` warnings are not failures.** Only exit status 2 and above is fatal.

4. **Init-container failures are first-class.** `PodStatus` reports init status,
   and the restart count is kept **separate** from the main container's. "The
   game crashed five times" and "the mod install failed five times" call for
   different responses, and an Incident that conflated them would mislead. The
   Incident summary names the failing step, and its log tail is captured from the
   init container.

5. **An unresolvable mod is refused at install time**, for both games, with the
   message distinguishing *"could not reach the index"* from *"no such mod"*.
   This is where the operator is present and nothing is broken yet. Boot-time
   validation remains as the backstop: a package can be deleted after it was
   installed, and the mod list can be hand-edited in the ops repository.

## Consequences

- A permanent failure means the World **crash-loops** rather than stopping: an
  init container cannot opt out of `restartPolicy: Always`. For that class the
  improvement is entirely visibility — the operator sees an Incident naming the
  mod instead of a pod stuck in `Init:CrashLoopBackOff`.

- **Retry and classification are Valheim-only.** Minecraft's downloads belong to
  itzg's image, which we do not control. Minecraft gains install-time validation
  and init visibility, but not retry. This asymmetry is accepted deliberately:
  owning Modrinth pack and dependency resolution ourselves is a large surface to
  take on for the sake of symmetry, and install-time validation removes the
  common cause anyway.

- **An index outage blocks installs.** Refusing on a resolver error means a
  Thunderstore or Modrinth outage prevents new installs. This is the right trade
  — not installing is recoverable, a crash-looping World is not — but it is only
  tolerable because the message distinguishes the two causes.

- The Incident record gains an init restart count, so anything reading
  `RestartCount` keeps its existing meaning.

- Reusing the existing `crash` event kind keeps the History timeline and its
  badge unchanged; the distinction lives in the Incident summary. If the timeline
  becomes noisy, splitting the kind later is cheap because the Incidents are
  stored either way.

## Alternatives considered

- **Skip the failing mod and start the World.** Rejected: a server whose mod set
  silently differs from the exported client profile produces desyncs and join
  failures that are far harder to diagnose than a server that refuses to start.

- **Retry everything, including 404s.** Rejected: it spends three attempts
  proving something the first response already established, and delays the
  report.

- **Rely on Kubernetes' backoff alone, with no in-script retry.** Rejected: it
  cannot distinguish causes, and a transient fault costs minutes of downtime.

- **Wrap Minecraft's mod installation in our own init container** for symmetry.
  Rejected as disproportionate, per the consequence above.
