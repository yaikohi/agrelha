# Running agrelha against a cluster it does not live in

What has to exist, and in what order, for a fresh agrelha to start managing game
worlds on a Kubernetes cluster it runs *outside* of.

Related: `adr/0007-per-instance-host.md` (why), `plans/0003-per-instance-host-and-agrelha-outside-the-cluster.md`
(build order and prerequisites), `yaya-ops/docs/agrelha-host-migration-cluster-impact.md`
(what moving an existing install does to the cluster).

## Read this first: one blocker, and one thing everyone misses

**Today agrelha cannot talk to an external cluster.** `internal/infra/kube/client.go`
calls `rest.InClusterConfig()` and nothing else, and there is no `KUBECONFIG`
knob in `internal/platform/config`. External operation needs slice 1 step 3 of
plan 0003 first. Everything else below works now.

**Managing a cluster needs two planes, not one.** This is the part that fails
quietly:

| Plane | Mechanism | Used for |
|---|---|---|
| Imperative | Kubernetes API | start, stop, restart, status, metrics, logs, backup Jobs |
| Declarative | git commit → **a reconciler applies it** | creating and deleting worlds, mods, configs, resources |

agrelha never applies a manifest itself. It commits to the ops repo and something
else — ArgoCD, Flux, a sync loop — converges the cluster. **If nothing is
watching the repo, world creation appears to succeed and no world appears.** The
commit lands, the UI reports success, and the cluster is untouched. Wire the
declarative plane before you trust anything the create flow tells you.

## 1. Cluster side

**A namespace per game.** Defaults are `valheim`, `minecraft-modded`, `gmod`,
overridable with `VALHEIM_NAMESPACE`, `MINECRAFT_NAMESPACE`, `GMOD_NAMESPACE`.

**A ServiceAccount agrelha authenticates as, plus a Role and RoleBinding in each
game namespace.** The grants agrelha actually needs: `pods` and `pods/log`
(read), `services` and `configmaps` (read), `deployments` and `deployments/scale`
(get/list/watch/patch), `metrics.k8s.io/pods` (read) for the resource tiles, and
`batch/jobs` (create) for backups and restores. No `exec`, no `secrets`, no
cluster-scoped rights.

The RoleBindings name the ServiceAccount **by namespace**. That namespace must
exist and keep existing even if agrelha itself never runs in the cluster —
deleting it silently voids every binding.

**A token for that ServiceAccount.** Kubernetes 1.24+ does not auto-create these;
declare a `kubernetes.io/service-account-token` Secret annotated with
`kubernetes.io/service-account.name`. The manifest contains no secret material —
the API server fills in `.data.token` — so it is safe to commit.

**Storage classes and a backups volume.** Worlds use node-local storage
(`local-path` here) and backups land on a shared volume the backup Job mounts.

**Addresses for the game Services.** Each world gets its own LoadBalancer
address. Here that is a Cilium L2 pool; anything that assigns stable addresses
on a reachable network works. Note `externalTrafficPolicy: Cluster` — with
`Local`, an announcement from a node that isn't running the pod yields a dead
address.

**An API endpoint reachable from wherever agrelha runs**, with a certificate
valid for the address you'll put in the kubeconfig.

## 2. Declarative plane

**A git repository agrelha can push to**, and a token with write access
(`GIT_REPO_URL`, `GIT_BRANCH`, `GIT_USERNAME`, `GIT_TOKEN`). Without these
agrelha starts read-only and logs
`GIT_REPO_URL/GIT_TOKEN unset: declarative plane is read-only`.

**A reconciler watching the per-game instance paths** — `VALHEIM_INSTANCES_PATH`,
`MC_INSTANCES_PATH`, `GMOD_INSTANCES_PATH`. One ArgoCD Application per game,
pointed at that path with recursion on, is the arrangement here. Give it
`CreateNamespace=true` so a new game namespace appears on first sync.

Leave `prune: true` on deliberately: deleting a world removes its directory from
git, and pruning is what makes the objects go away.

## 3. Identity

agrelha fails closed. The provider is chosen in this order:

1. `OIDC_ISSUER` set → OIDC. **Discovery failure is fatal and the process
   refuses to start** — deliberate, so a transient outage cannot silently
   degrade the deployment to passwordless.
2. `AUTH_MODE=dev` → passwordless. Local development only; it logs a warning
   saying so.
3. Local user accounts in the database, if any exist.

For OIDC you need an application at the identity provider with the redirect URI
matching `OIDC_REDIRECT_URL` exactly, and — for per-world Roles — a project whose
roles are asserted into the token. Set `ZITADEL_PROJECT_ID`, `ZITADEL_API_URL`
and `ZITADEL_SERVICE_KEY` for agrelha to mint and retire a Role per world.

The role claim must actually be emitted. In Zitadel that is the application
setting **"User roles inside ID token"** — the similarly-named access-token
option is not the one that matters. With the claim absent, every sign-in has zero
roles and nobody is authorized.

## 4. agrelha side

**Persistent storage for `DB_PATH`** — sessions, accounts, tiers, budgets and
requests live there. Losing it logs everyone out and resets settings to the
seeded defaults.

**Secrets:** `GIT_TOKEN`, `OIDC_CLIENT_ID`, `OIDC_CLIENT_SECRET`,
`ZITADEL_SERVICE_KEY`, and optionally `CURSEFORGE_API_KEY` and
`STEAM_WEB_API_KEY`. Both API keys degrade gracefully when empty — CurseForge
search is simply absent, and Steam Workshop collections stop resolving — but an
empty value is very different from the key being *missing* from a secret store
that builds its target atomically.

**Per-game placement and sizing:** `GAME_NODE_SELECTOR` and `GAME_NODE_NAME` pin
worlds to a node; `*_LB_BASE_IP` sets where each game's addresses start;
`*_TOTAL_BUDGET_GIB`, `*_MAX_INSTANCES`, `*_MAX_RUNNING` bound each game.

**Budgets are seeded once.** They are read from the environment the *first* time
agrelha sees a game and stored in SQLite; after that the stored values win and
the environment is never consulted again. Changing the env var later does
nothing — edit the settings in `/overview` instead.

## 5. Bring-up order

1. Create the namespaces, ServiceAccount, Roles, RoleBindings and token Secret.
2. Build a kubeconfig from the token and **verify it in both directions** — it
   can read pods in each game namespace, and is refused `get nodes` and secrets
   elsewhere. A non-expiring token with an over-broad grant is a standing risk.
3. Create the ops repo and the write token. Commit the per-game directories.
4. Point a reconciler at each game path and confirm it syncs an empty directory
   cleanly.
5. Configure the identity provider; confirm the role claim appears in a decoded
   token **before** cutting over, or nobody can sign in.
6. Start agrelha with the kubeconfig, git credentials and OIDC settings.
7. Create one throwaway world end to end, then delete it.

Step 7 is the only honest test, because it is the only one that exercises both
planes together.

## 6. What breaks quietly

**No reconciler.** Creation succeeds, nothing appears. The commit is in the repo
and the cluster never hears about it.

**Deleted agrelha namespace.** Every RoleBinding references the ServiceAccount by
namespace; remove it and all cluster access vanishes while the manifests still
look correct.

**Missing role claim.** Sign-in works, authorization does not, and the logs show
`roles=0`.

**Budget env edited after first boot.** Ignored. The stored value wins.

**`externalTrafficPolicy: Local`** with a node-pinned world. The address is
announced by a node that isn't running the pod, so it answers nothing and there
is no error anywhere.

**A missing property in an atomically-built secret.** An *empty* API key is a
supported state; a *missing* key can fail the whole secret and leave the
container unable to start.
