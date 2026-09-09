# Configuration

agrelha reads everything from the environment, and every key has a default. A
container started with **no variables at all** comes up as a Kubernetes panel
using in-cluster credentials, local accounts and a SQLite database on `/data`.

## Required

**Nothing** — with one exception:

| Key | When required |
|---|---|
| `RUNTIME` | Set to `docker` when not running in Kubernetes. Defaults to `k8s`. |

Everything below is optional and enables a capability.

## Turning things on

### Declarative plane (git)

Editing mods, configs and access writes to a git repository, which your GitOps
controller then reconciles. Both keys are needed — with either one missing the
panel runs read-only and write attempts report that the plane is unconfigured.

| Key | Default | Notes |
|---|---|---|
| `GIT_REPO_URL` | — | HTTPS clone URL |
| `GIT_TOKEN` | — | Secret. `CODEBERG_TOKEN` also accepted |
| `GIT_USERNAME` | — | Secret. `CODEBERG_USERNAME` also accepted |
| `GIT_BRANCH` | `main` | |
| `GIT_AUTHOR_NAME` | `agrelha` | |
| `GIT_AUTHOR_EMAIL` | `agrelha@localhost` | |

With `RUNTIME=docker` (or `LOCAL_STATE_DIR` set) the same state is written to
disk instead and applied with compose; git is not involved.

### Authentication

With no OIDC configured agrelha uses local accounts. If there is no account yet
it accepts a passwordless sign-in so you can create one — do that before the
panel is reachable by anyone else.

| Key | Default | Notes |
|---|---|---|
| `OIDC_ISSUER` | — | Enables OIDC when set |
| `OIDC_CLIENT_ID` | — | |
| `OIDC_CLIENT_SECRET` | — | Secret |
| `OIDC_REDIRECT_URL` | — | `https://<host>/auth/callback` |
| `OIDC_POST_LOGOUT_URL` | — | |
| `ALLOWED_EMAIL` | — | Comma-separated allow-list. Empty means every issuer identity is accepted |

### Where servers live

| Key | Default |
|---|---|
| `VALHEIM_NAMESPACE` | `valheim` |
| `VALHEIM_DEPLOYMENT` | `valheim` |
| `VALHEIM_ADDRESS` | — (shown to players) |
| `VALHEIM_STATUS_URL` | — (lloesche status sidecar) |
| `MINECRAFT_NAMESPACE` | `minecraft-modded` |
| `MC_INSTANCES_PATH` | `manifests/minecraft-modded` |
| `MC_LB_BASE_IP` | — (per-instance LB IPs are allocated from this base) |
| `GAME_NODE_SELECTOR` | — e.g. `example.org/gameserver=true` |
| `GAME_NODE_NAME` | — cosmetic |

### Capacity

| Key | Default | Notes |
|---|---|---|
| `MAX_INSTANCES` | `4` | `MC_MAX_INSTANCES` also accepted |
| `MAX_RUNNING` | `2` | `MC_MAX_RUNNING` also accepted |
| `TOTAL_BUDGET_GIB` | `24` | `MC_TOTAL_BUDGET_GIB` also accepted |

### Runtime and storage

| Key | Default |
|---|---|
| `RUNTIME` | `k8s` (`k8s` or `docker`) |
| `DOCKER_SOCKET` | `/var/run/docker.sock` |
| `COMPOSE_DIR` | `compose` |
| `LOCAL_STATE_DIR` | `/data/state` when the local plane is active |
| `DB_PATH` | `/data/agrelha.db` |
| `LISTEN_ADDR` | `:8080` |
| `BACKUPS_DIR` | — a directory to report last-backup time and size from |

### Provenance

| Key | Default |
|---|---|
| `SOURCE_URL` | the upstream repository |

agrelha's footer links here. If you modify agrelha and host it for other people,
AGPL section 13 obliges you to offer them the corresponding source — set this to
your own repository and the footer does it for you.

### Content sources and observability

| Key | Default |
|---|---|
| `MODRINTH_API` | `https://api.modrinth.com/v2` |
| `THUNDERSTORE_API` | `https://thunderstore.io/c/valheim/api/v1` |
| `INFLUXDB_URL` | — enables the metrics tiles |
| `GRAFANA_DASHBOARD_URL` | — adds a deep-link |
| `LOG_LEVEL` | `info` (`debug` traces SSE frames) |
| `LOG_FORMAT` | `text` (`text` or `json`) |

### In-game administration over RCON (Minecraft)

| Key | Default |
|---|---|
| `MINECRAFT_RCON_ADDR` | `minecraft-modded.minecraft-modded.svc.cluster.local:25575` |
| `MINECRAFT_RCON_PASSWORD` | — RCON is disabled while unset |

### Paths inside the state repository

Defaults suit a repository laid out like this one; change them to match yours.

| Key | Default |
|---|---|
| `MODS_PATH` | `manifests/valheim-mods.yaml` |
| `ADMINS_PATH` | `manifests/valheim-admins.yaml` |
| `MOD_CONFIGS_PATH` | `manifests/valheim-mod-configs.yaml` |
| `MINECRAFT_MODS_PATH` | `manifests/minecraft-modded/mods.yaml` |
| `MINECRAFT_ACCESS_PATH` | `manifests/minecraft-modded/access.yaml` |
| `MINECRAFT_CONFIGS_PATH` | `manifests/minecraft-modded/configs.yaml` |
