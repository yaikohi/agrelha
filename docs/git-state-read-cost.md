# Follow-up: every git state read clones the whole repository

**Status**: identified, not fixed. Written up while building per-instance mod
update detection, which had to be designed around it.

## What happens today

`ports.StateStore.Get` is satisfied in production by `internal/infra/state/git`,
which delegates to `gitops.Committer.ReadFile` (`internal/infra/gitops/data.go:227`):

```go
dir, err := os.MkdirTemp("", "agrelha-git-read-")
defer os.RemoveAll(dir)
_, err = git.PlainCloneContext(ctx, dir, false, &git.CloneOptions{
    URL: c.RepoURL, Auth: auth,
    ReferenceName: plumbing.NewBranchReferenceName(c.Branch),
    SingleBranch: true, Depth: 1,
})
full := filepath.Join(dir, relPath)
return os.ReadFile(full)
```

Reading one document is therefore: create a temp directory, clone the branch
over the network, read a few hundred bytes, delete the clone. Nothing is reused
between reads — two reads in the same request pay for two clones.

## Why it matters

The cost is invisible at the call site. `m.stateStore.Get(ctx, path)` reads like
a map lookup, and it appears in `GetInstalledMods`, `GetConfig`, `ListConfigs`,
the global-configs readers and `checkInstanceDirFree` (the slug-collision guard
on the create path). Anything that loops over instances multiplies clones by the
instance count.

`ReadFile` is not the only offender. Six call sites across `gitops` clone into a
fresh temp directory: `committer.go:32`, `data.go:81`, `data.go:228`, and both
directory helpers at `dir.go:21` and `dir.go:105`. So a read-modify-write is two
clones, and creating an instance is several.

Two things currently hide the damage:

- In the cluster, the Valheim and Minecraft managers are wired with
  `WithModsReader` / `WithConfigsReader`, which read the rendered ConfigMap
  through the Kubernetes API instead. The git path is the fallback, so it is
  mostly exercised in local and Docker-runtime setups — exactly where it is
  least likely to be noticed and most likely to be slow.
- Reads that stayed on the git path are on pages nobody loads in a tight loop.

Neither is a property of the adapter. The next feature that reads declared state
per instance, per render, pays full price. Mod update detection would have: a
badge on four world cards is four clones per page view. It was built around a
background refresher on a ticker for that reason, which is the right shape
anyway, but the constraint drove the design rather than the requirement doing so.

## The fix

Keep one working clone per `Committer` and update it instead of recreating it:

1. Clone once, lazily, into a directory that outlives the call (a configured
   path, or one temp directory per process).
2. On read, fetch and hard-reset that clone to the remote branch tip, then read
   from the worktree. `go-git`'s `Fetch` + `Reset` on an existing repository
   transfers only new objects.
3. Guard the worktree with a mutex — `Patch` already serialises writes, and reads
   must not observe a half-applied commit.
4. Cache the resolved head SHA for a short TTL so a burst of reads in one request
   fetches once, the same way `kube.ServiceIP` caches for 60s.

Correctness does not change: the adapter still answers from the remote branch,
which is what ArgoCD reconciles against.

## What to watch for

- **Staleness.** A cached head makes a read lag a commit made elsewhere. That is
  already true — the current code reads whatever the clone happened to fetch —
  but a TTL makes the window explicit and needs to be shorter than the ArgoCD
  sync interval to stay useful.
- **Disk.** A persistent clone is a persistent PVC concern in the cluster. The
  repository is small; say so in the sizing rather than assuming it.
- **Concurrent writes.** `Patch` currently clones its own copy, so a failed
  commit leaves nothing behind. Sharing the worktree means a failed write must
  reset it, not leave it dirty for the next reader.
