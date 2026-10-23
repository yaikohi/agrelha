# 8. Convergence is level-triggered on a Docker Host

Date: 2026-10-21

## Status

Accepted.

## Context

Applying a Mod update writes the new Mod list to Desired state in git and then
expects the world to restart with it.

In the cluster that is enough. ArgoCD watches the repository, materialises the
change into a ConfigMap, and the Instance restarts. agrelha commits and walks
away.

On a Docker Host nothing watches git. agrelha is the only thing that can carry
Desired state onto the Host's disk, and `ReplaceMods` did not: it patched
`mods.yaml`, recorded an audit row, and returned. The Materialised state stayed
as it was, the one-shot mod reconciler never re-ran, and the world kept serving
the mods it had loaded at boot — while the UI reported success.

Adding a converge to `ReplaceMods` fixed the first apply and nothing after it,
because the converge sat behind the early return for "Desired state did not
change". Once an earlier apply had committed, Desired was already correct and
every retry was a no-op — with the disk still stale. The design was
edge-triggered: it reacted to Desired *changing*, and a stale Materialised state
is not an edge, it is a condition.

## Decision

**On a Docker Host, applying Desired state always converges, whether or not
Desired changed.** The trigger is the condition "Materialised may not match
Desired", not the event "Desired changed".

The cluster path is unchanged: it still commits and leaves convergence to ArgoCD.

## Consequences

Convergence became safe to run unconditionally because of two properties that
existed for other reasons, and that is the part worth remembering:

**No empty commits.** `gitops.WriteDirectory` stages the rendered tree and
inspects `wt.Status()` before committing; with nothing modified it returns
without creating a commit. Re-converging an unchanged world therefore adds
nothing to the ops-repo history.

**No needless restarts.** The rendered Compose file carries an
`agrelha.mods-digest` label derived from the Mod list's content, on both the
server and the one-shot reconciler. An unchanged Mod list renders byte-identical
output, so `docker compose up -d` is a genuine no-op. A changed one changes the
label, which is what makes Compose recreate both services and re-run the
reconciler — `mods.txt` is a bind-mounted file and is otherwise invisible to
Compose's change detection, so without the label a correct converge would still
have left the old mods running.

Remove either property and unconditional convergence becomes expensive or
destructive. A test pins each.

**A renderer that omits a file deletes it.** `ports.StateStore.PutTree` removes
files in the directory it was not given, so converging more often made the
Compose renderer's omission of `mods.yaml` and `configs.yaml` dangerous rather
than merely untidy — it would have deleted the Mod list from git on the next
apply. Both renderers now emit those files from one shared definition, and a
test asserts their output is byte-identical across Hosts.

## Alternatives considered

**Keep it edge-triggered and repair staleness elsewhere** — a reconcile loop, or
a check at start. Rejected: it needs a second mechanism to notice a condition the
apply path is already standing in front of, and the failure it guards against is
silent, so the loop's own failures would be silent too.

**Compare Materialised against Desired and converge only on a difference.**
Correct in principle and strictly cheaper, but `ports.Materializer` is
write-only; adding a read to it to avoid a no-op that `WriteDirectory` and the
digest label already make free is machinery for nothing.

**Have something on the Host pull from git**, mirroring ArgoCD. Rejected in ADR
0007: it reintroduces a background agent and a second thing to trust, when the
only writer is already in the room.
