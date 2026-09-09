# Session handoff — 2026-09-09 (modularization phases 0–8)

Continues from `SESSION-HANDOFF-2026-09-07.md`. Work follows
[docs/modularization-plan.md](modularization-plan.md); vocabulary follows
[CONTEXT.md](../CONTEXT.md). **Verify with `git status` before trusting this.**

---

## 1. State at handoff

| | |
|---|---|
| `agrelha` HEAD | `6bace47 chore: restructured` |
| Tests | 176 test functions across 43 packages (31 have tests); `go vet` and `-race` clean; coverage 33.6% |
| Cluster | healthy: `mc-ayyy-01` Running/ready/0 restarts, Valheim up, agrelha `0.23.4` |

### Uncommitted in agrelha
```
M plan.md
M docs/SESSION-HANDOFF-2026-09-09.md
```

Nothing needs deploying — the k8s adapter delegates to the same client, so
runtime behaviour is unchanged.

---

## 2. Done this session

### Phase 0 — hygiene ✅
- `CODEBERG_*` → `GIT_*`, **reading the old names as a fallback** so the running
  deployment's Secret keeps working.
- Removed dead code: `fabMods` (assigned, never read), `NewFabricModManager`,
  `SetAltDeployment`/`altDeployment`, `ConfigMapMeta`, and the
  `FABRIC_DEPLOYMENT` / `FABRIC_MODS_PATH` / `MINECRAFT_SLOT_PATH` config.
- Made configurable: LB base IP, node selector, node name, instances path,
  Valheim address, **and the namespace** — which all six manifest templates had
  hardcoded as `minecraft-modded`.
- Annotation prefix `agrelha.ykhi.xyz/` → `agrelha.dev/`. Safe because Instance
  state is rebuilt from SQLite (`InstanceFromRecord`), never from cluster
  annotations, so they are write-only documentation.
- **Defaults now suit a stranger:** `GAME_NODE_SELECTOR` and `MC_LB_BASE_IP`
  default to empty (schedule anywhere, let the LB allocate), and the Service
  template omits `loadBalancerIP` entirely when unset. Your behaviour is
  preserved *explicitly* in `manifests/agrelha-app.yaml` — that file is what
  stops instances being scheduled off `game-01`.

### Phase 1 — Runtime port ✅
- `internal/ports/runtime.go`: `Start`/`Stop`/`Restart`/`Status`/`Metrics`/`Logs`.
  `Status` carries `Lifecycle` and `Available` separately, per CONTEXT.md.
- `internal/adapters/runtime/k8s` — wraps the existing client.
- `internal/adapters/runtime/docker` — compiles, type-checks against the port,
  returns `ErrNotImplemented`. The `var _ ports.Runtime` line is a tripwire: if
  the port grows a Kubernetes-shaped concept, this file stops compiling.
- **`minecraft` no longer imports `k8s`** — the phase goal. Only `gitops`
  remains, which is phase 3.

Judgement calls worth not relitigating:
- **`RunTask` was left out of the port.** Its only implementation is
  `CreateBackupJob(jobName, archiveName, dataClaimName, backupsClaimName)` — PVC
  claim names, a Kubernetes volume model. It waits for the volume model.
- **`ConfigMapData` (22 call sites) is not a Runtime concern** — it reads
  declarative state. It belongs to the StateStore port in phase 3.
- The port's Lifecycle/Available split turned out to express exactly the three
  states `InstanceManager` already derived from `(desired, ready)` replica
  counts — evidence the split is real, not invented.

### Phase 2 — test coverage, rescoped ✅
Measured **23.8% overall** before, now **28.2% overall** (127 passing tests).
Targeted critical packages are now covered:
- **`gitops` (79.7%)**: `committer_test.go` exercises `Patch`, `SetData`, `DeleteData`,
  `ReplaceData`, `ReplaceConfigMap`, `WriteDirectory`, `DeleteDirectory`, and
  explicitly verifies the **no-op path** creates no commit.
- **`store` (82.1%)**: `store_test.go` exercises `migrate()` idempotency,
  player roster `COALESCE(NULLIF(excluded.character, ''), players.character)`
  upsert preserving known metadata, presence online counts, instance CRUD,
  history filtering, and mod index/readme caching.
- **`adapters/runtime/k8s` (94.1%)**: `runtime_test.go` exercises start/stop/restart
  replica scaling and template annotation patch, and asserts that status
  `Lifecycle` derives from desired replicas and `Available` from pod readiness
  condition, never from pod `Phase`.
- **`minecraft` invariants (45.0%)**: `invariants_test.go` verifies resource tiers
  memory mapping, disjoint `Env()` key sets per source (CurseForge vs Modrinth vs
  Vanilla vs Fabric vs NeoForge), RAM budget overcommit rejection, and max instance limit.
- **`config` (100%)**: `config_test.go` tests defaults, `GIT_*` fallback/precedence
  over `CODEBERG_*`, and unconstrained empty values.
- Added `task test:cover` in `Taskfile.yml`.

---

### Phase 3 — State + Reconciler ✅
- **Defined ports**:
  - `ports.Document`: `Data map[string]string`, `Annotations map[string]string`, `Raw []byte`.
  - `ports.StateStore`: `Get`, `Put`, `Patch`, `Delete`, `PutTree`.
  - `ports.Reconciler`: `Converge(ctx, ref)`, `Async() bool`.
- **Implemented adapters**:
  - `internal/adapters/state/git/`: implements `ports.StateStore` wrapping `gitops.Committer`.
  - `internal/adapters/state/local/`: compiling skeleton returning `ErrNotImplemented`.
  - `internal/adapters/reconcile/argocd/`: implements `ports.Reconciler` (`Async() == true`).
  - `internal/adapters/reconcile/compose/`: compiling skeleton returning `ErrNotImplemented` (`Async() == false`).
- **Inverted domain dependencies**:
  - `internal/admins`, `internal/mods`, and `internal/minecraft` no longer import `gitops` or `k8s`.
  - **No domain package imports infrastructure.**

---

### Phase 4 — Domain extraction ✅
- **Created pure domain package `internal/domain` (90.8% coverage)**:
  - `domain.GameID`, `domain.AdmissionModel`, `domain.OperatorIDKind`, `domain.Display`.
  - `domain.ResourceTier` (`TierSmall`, `TierMedium`, `TierLarge`), memory calculations, normalization.
  - `domain.Source`, `domain.Provider`, `domain.Loader`, `domain.Pack`, `domain.Slugify`, normalization.
  - `domain.Instance`, `domain.InstanceState`, invariant rules (`PackDefined`, `CanSetLoader`, `CanSetVersion`, `PackOwnedFieldErr`), manifest naming and env/annotation helpers.
  - `domain.Budget`, `domain.CalculateBudget`, `CanStart`, `CanCreate`, `AddUsage` (global multi-game RAM budget & instance limits).
  - Pure domain data contracts: `domain.Bundle`, `domain.ContentItem`, `domain.ContentSet`, `domain.PortSpec`, `domain.VolumeSpec`, `domain.RuntimeSpec`.
  - **`internal/domain` imports nothing outward** (stdlib only: `fmt`, `regexp`, `strconv`, `strings`, `time`).
- **Linked `internal/minecraft` to `domain`**:
  - Aliased types and constants (`type Instance = domain.Instance`, `type ResourceTier = domain.ResourceTier`, etc.).
  - Replaced manual budget checks in `InstanceManager` (`CreateInstance`, `StartInstance`, `Budget`) with `domain.CalculateBudget`, `budget.CanCreate()`, and `budget.CanStart(*inst)`.
  - Added conversion helpers `InstanceFromRecord` and `InstanceToRecord`.
  - Supported global budget env configuration (`TOTAL_BUDGET_GIB`, `MAX_INSTANCES`, `MAX_RUNNING`) with fallback to `MC_*`.

---

### Phase 5 — Game interface ✅
- **Defined `ports.Game` and `ports.ContentProvider` in `internal/ports/game.go`**:
  - `ID() domain.GameID`, `Display() domain.Display`.
  - `Providers() []ContentProvider`, `ResolveContent(...)`, `ExportClientBundle(...)`.
  - `RuntimeSpec(inst) domain.RuntimeSpec`.
  - `AdmissionModel() domain.AdmissionModel`, `OperatorIDKind() domain.OperatorIDKind`.
- **Implemented game engines under `internal/games/`**:
  - `internal/games/valheim` (82.4% coverage): implements `ports.Game`, extracts `WithBepInEx` and `.r2z` bundle creation from `server/handlers_modpack.go`.
  - `internal/games/minecraft` (73.1% coverage): implements `ports.Game`, generates `.mrpack` bundle using `modpack.BuildMrpack`.
  - Both games thoroughly tested against the `ports.Game` interface.
- **Handler extraction**:
  - Extracted Valheim BepInExPack injection from `FiberServer.withBepInEx` to `games/valheim.WithBepInEx`.

---

### Phase 6 — Auth port ✅
- **Defined `ports.Auth` and `ports.UserStore` in `internal/ports/auth.go`**:
  - Web contract: `IsAuthenticated(*fiber.Ctx) bool`, `Middleware() fiber.Handler`, `Login(*fiber.Ctx) error`, `Callback(*fiber.Ctx) error`, `Logout(*fiber.Ctx) error`.
  - Persistence contract: `ports.UserStore` with `GetUser`, `CreateUser`, `ListUsers`, `DeleteUser`.
- **Implemented adapters**:
  - `internal/adapters/auth/oidc/` (74.7% coverage): OIDC provider authentication, CSRF nonce verification, session cookie signing, Datastar SSE redirect support, and comma-separated `ALLOWED_EMAIL` admin list matching.
  - `internal/adapters/auth/local/` (81.0% coverage): Argon2id password hashing (`m=64MB, t=1, p=4`), constant-time hash verification, HTML login form rendering, and session issuance.
  - `internal/store/users.go`: SQLite implementation of `ports.UserStore` with idempotent migrations and CRUD tests.
- **Server integration**:
  - `FiberServer.auth` field transitioned to `ports.Auth`.
  - `server.New()` automatically detects existing local users in SQLite store when OIDC is unset and activates local authenticator, falling back to local dev 1-click authenticator for zero-friction development.
  - Added `server.WithAuth(ports.Auth)` option for dependency injection.
  - Registered `POST /auth/login` for local credential submission.

### Phase 7 — HTTP split ✅
- **Created feature packages under `internal/http/`**:
  - `internal/http/shared`: rendering, flash cookies, formatting, actor context, structured logging, SSE toast notifications.
  - `internal/http/access`: admission & operator management for Valheim and Minecraft, audit history.
  - `internal/http/backups`: backup status, manual snapshot creation, restore, download, deletion.
  - `internal/http/console`: Valheim log streaming & server restart/stop, Minecraft live log streaming & interactive RCON command execution.
  - `internal/http/dashboard`: public & admin landing page, live Datastar tile signals, updates SSE stream.
  - `internal/http/content`: Valheim mods, mod configs, update checking & bulk application, `.r2z` bundle export, image proxy.
  - `internal/http/instances`: Minecraft dashboard, instance lifecycle, detail page, settings, modpack wizard & cart provisioning, instance configs, daily backup scheduler.
- **Strangler migration**:
  - All original `internal/server` handlers converted into thin shims delegating to `internal/http/*`.
  - Lazy initialization pattern (`ensure*Handler()`) ensures backward compatibility with direct `&FiberServer{}` construction in legacy test suites.
  - Legacy tests (46 integration test suites in `internal/server`) continue to pass without modification.
- **Rules satisfied**:
  - `internal/server` fell from 5,453 to **1,215** non-test LOC.
  - The ~800 LOC rule is **not fully met**: `internal/http/instances` (1,739),
    `internal/server` (1,215) and `internal/http/content` (841) still exceed it.
    Outside this phase's scope, `cmd/web/pages` is now the largest package in the
    repo at ~6,700 LOC.
  - Test suite and race detector clean.

### Phase 8 — Docker adapter ✅
- **Implemented `internal/adapters/state/local`**:
  - Satisfies `ports.StateStore` using local files and SQLite revision history (`state_history` table).
  - Preserves no-op guarantee: identical writes trigger zero filesystem writes or revision logs.
  - Supports point-in-time document rollback and protects against path traversal.
- **Implemented `internal/adapters/reconcile/compose`**:
  - Satisfies `ports.Reconciler` with immediate, synchronous convergence (`Async() == false`).
  - `RenderCompose`: converts domain runtime specs into valid `docker-compose.yml`.
  - Executes `docker compose up -d --remove-orphans` through customizable executor.
- **Implemented `internal/adapters/runtime/docker`**:
  - Satisfies `ports.Runtime` via Docker Engine REST API using Go standard library `net/http` and unix socket (`/var/run/docker.sock`), requiring no external SDK/CGO dependencies.
  - Implements container start, stop, restart, inspect, stats, and stream-demuxed logs.
  - Accurately derives `ports.Status` lifecycle and health-check availability, and computes CPU millicores and memory MiB.
- **Server integration**:
  - Added `RUNTIME=docker`, `DOCKER_SOCKET`, `LOCAL_STATE_DIR`, `COMPOSE_DIR` configuration options.
  - Wired into `server.New()` with automatic fallback to Docker runtime and local state store when Git/Kubernetes are unset.
- **Test suite**: 176 test functions across 43 packages, race detector clean.

---

## 3. Exactly where to pick up: Phase 9 (Packaging)

Next is **Phase 9**: Packaging — make installing agrelha simple for both Audience 1 and Audience 2 without reading Go source code:
- **Audience 2 (Docker / Compose)**: Provide a clean, self-contained `docker-compose.yml` for agrelha with its volume mounts (`/data`), local state directory, and optional container runner configuration.
- **Audience 1 (Kubernetes)**: Provide a clean Helm chart (or updated kustomize manifests) packaging deployment, service, PVC, and ingress/gateway rules.
- Shrink required config variables toward ~5 essentials with sensible defaults across both environments.

---

## 4. Traps already paid for — do not re-derive

- **`subPathExpr` is unusable on this cluster** (k8s v1.36 + local-path): it
  resolves to a root-owned directory not on the PVC. World isolation uses itzg's
  `LEVEL`. Do not reintroduce subPath for `/data`.
- **ArgoCD `selfHeal` reverts every `kubectl apply` within ~1 min.** Nothing
  sticks until committed — do not debug by applying live.
- **Go templates resolve fields at runtime.** A rename that `go build` and
  `go vet` both accept still breaks manifest rendering. This already happened
  once (`SlotCMName`); only a test caught it.
- **`capabilities: drop: ["ALL"]` removes `CAP_CHOWN`** — a root init container
  still fails to chown.
- **An `nfs:` PV whose export path does not exist blocks the pod entirely**
  (`exit status 32`). It is not inert when unused.
- **CurseForge packs can block third-party distribution**; no API key helps.
- **modpackindex's public API exposes no loader field** — the loader is derived
  from which loaders a pack's mods support. A naive per-loader tally is
  misleading (Fluxweave reports 117 fabric-capable mods yet is NeoForge-only);
  the gate keys on what the target loader *cannot* run.
- **The vanilla `minecraft` namespace is not agrelha's.** Its RBAC is namespaced
  to `minecraft-modded` only. Leave it alone.

---

## 5. Open, unresolved

- **The name.** "Agrelha" is one Valheim server's name; odd for a multi-game
  panel. Cheapest to change before publication.
- **Licence** — not chosen.
- Whether Valheim multi-instance is actually wanted now that cardinality is
  operator config rather than a game property.

---

## 6. Commands

```sh
cd ~/projects/agrelha
go build ./... && go test ./... && go vet ./...
go test ./... -coverprofile=/tmp/cov.out && go tool cover -func=/tmp/cov.out | tail -1
go test ./internal/adapters/gitops/ -v   # the offline git harness
kubectl get pods -n minecraft-modded
```
