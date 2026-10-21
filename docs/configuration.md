# Configuration

agrelha reads everything from the environment, and every key has a default. A
container started with **no variables at all** comes up as a Kubernetes panel
using in-cluster credentials, local accounts and a SQLite database on `/data`.

## Choosing where worlds run

A world records the **Host** it runs on. Two are possible, and one agrelha can
serve both at once.

| | Kubernetes | Docker Host |
|---|---|---|
| Runtime | in-cluster API, or `KUBECONFIG` | Docker socket |
| Renderer | Kubernetes manifests | Compose file |
| Convergence | ArgoCD, asynchronously | `docker compose up`, immediately |
| Desired state | git | git — the same repository |

Git is the desired state for **both**. A Docker Host additionally keeps a local
copy of its rendered files, because Compose needs them on disk; that copy is a
cache, and git wins if they diverge.

### Running against Kubernetes only

The default. Nothing is required if agrelha runs inside the cluster. From
outside it, set `KUBECONFIG` — agrelha prefers it over in-cluster credentials,
so running outside is a deliberate act rather than an accidental fallback.

```sh
KUBECONFIG=/etc/agrelha/kubeconfig      # only when running outside the cluster
GIT_REPO_URL=https://...                # or the panel is read-only
GIT_TOKEN=...
```

### Running against a Docker Host

Name the Host, point at the socket, and say where files live. Without
`DOCKER_HOST_NAME` no Docker Host is registered, and a world that declares one
is **refused** rather than silently rendered as Kubernetes YAML.

```sh
DOCKER_HOST_NAME=fogo     # the Host name worlds record
DEFAULT_HOST=fogo         # new worlds land here instead of the cluster
DATA_DIR=/srv/gamedata    # world data; MUST be outside COMPOSE_DIR
COMPOSE_DIR=/srv/compose  # rendered Compose files, mirrors the git layout
DOCKER_SOCKET=/var/run/docker.sock
```

`DATA_DIR` must not sit inside `COMPOSE_DIR`. The Compose directory is
materialised from git, so world data underneath it would be destroyed by a
`git clean` or a checkout.

Compose is invoked with an explicit project name per world, so two games'
`instance-01` cannot collide and tear down each other's containers.

### Serving both at once

Set the Docker keys above and leave `DEFAULT_HOST` unset (or `cluster`). New
worlds stay in the cluster; a world whose Host is the Docker Host routes to
Compose. Per-game renderers are selected the same way.

## Required

**Nothing** — with one exception:

| Key | When required |
|---|---|
| `RUNTIME` | Set to `docker` when agrelha itself is not running in Kubernetes. Defaults to `k8s`. |

## Host routing

| Key | Default | |
|---|---|---|
| `DOCKER_HOST_NAME` | — | Registers a Docker Host under this name. Unset means cluster-only |
| `DEFAULT_HOST` | — | Host recorded on newly created worlds. Empty means `cluster` |
| `DATA_DIR` | `/srv/gamedata` | World data root on a Docker Host |
| `COMPOSE_DIR` | `compose` | Where rendered Compose files are materialised |
| `DOCKER_SOCKET` | `/var/run/docker.sock` | |
| `KUBECONFIG` | — | Use these credentials instead of in-cluster ones |

## Declarative plane (git)

Unset, agrelha starts read-only and logs
`GIT_REPO_URL/GIT_TOKEN unset: declarative plane is read-only`.

| Key | Default | |
|---|---|---|
| `GIT_REPO_URL` | — | HTTPS clone URL |
| `GIT_TOKEN` | — | Secret. `CODEBERG_TOKEN` also accepted |
| `GIT_USERNAME` | — | `CODEBERG_USERNAME` also accepted |
| `GIT_BRANCH` | `main` | |
| `GIT_AUTHOR_NAME` | `agrelha` | |
| `GIT_AUTHOR_EMAIL` | `agrelha@localhost` | |
| `LOCAL_STATE_DIR` | — | Filesystem state instead of git. Development only |

Something must apply what agrelha commits. **If nothing watches the repository,
creating a world succeeds and no world appears** — the commit lands and the
cluster never hears about it.

## Authentication

Chosen in this order: OIDC if `OIDC_ISSUER` is set; then `AUTH_MODE=dev`; then
local accounts if any exist. **OIDC discovery failure is fatal** and the process
refuses to start, so a transient outage cannot quietly degrade a deployment to
passwordless.

| Key | Default | |
|---|---|---|
| `OIDC_ISSUER` | — | Enables OIDC when set |
| `OIDC_CLIENT_ID` | — | |
| `OIDC_CLIENT_SECRET` | — | Secret |
| `OIDC_REDIRECT_URL` | — | `https://<host>/auth/callback` |
| `OIDC_POST_LOGOUT_URL` | — | |
| `AUTH_MODE` | — | `dev` is passwordless. Never outside local development |

### Per-world roles (Zitadel)

| Key | Default | |
|---|---|---|
| `ZITADEL_API_URL` | — | |
| `ZITADEL_PROJECT_ID` | — | Empty disables role minting; worlds are created but creators are not granted access |
| `ZITADEL_SERVICE_KEY` | — | Secret. The whole JWT-profile key JSON |

The identity provider must actually emit the roles claim. In Zitadel that is the
application's **"User roles inside ID token"** setting; without it every sign-in
carries zero roles and nobody is authorized.

## Where servers live

| Key | Default |
|---|---|
| `VALHEIM_NAMESPACE` | `valheim` |
| `VALHEIM_DEPLOYMENT` | `valheim` |
| `VALHEIM_INSTANCES_PATH` | `manifests/valheim` |
| `VALHEIM_LB_BASE_IP` | — per-world addresses are allocated from this base |
| `VALHEIM_ADDRESS` | — shown to players |
| `VALHEIM_STATUS_URL` | — lloesche status sidecar |
| `MINECRAFT_NAMESPACE` | `minecraft-modded` |
| `MINECRAFT_DEPLOYMENT` | `minecraft-modded` |
| `MC_INSTANCES_PATH` | `manifests/minecraft-modded` |
| `MC_LB_BASE_IP` | — |
| `GMOD_NAMESPACE` | `gmod` |
| `GMOD_INSTANCES_PATH` | `manifests/gmod` |
| `GMOD_LB_BASE_IP` | `192.168.20.230` |
| `GAME_NODE_SELECTOR` | — e.g. `example.org/gameserver=true` |
| `GAME_NODE_NAME` | — cosmetic |

## Capacity

| Key | Default | |
|---|---|---|
| `MAX_INSTANCES` | `4` | `MC_MAX_INSTANCES` also accepted |
| `MAX_RUNNING` | `2` | `MC_MAX_RUNNING` also accepted |
| `TOTAL_BUDGET_GIB` | `24` | `MC_TOTAL_BUDGET_GIB` also accepted |
| `VALHEIM_MAX_INSTANCES` | `4` | |
| `VALHEIM_MAX_RUNNING` | `2` | |
| `VALHEIM_TOTAL_BUDGET_GIB` | `16` | |
| `GMOD_MAX_INSTANCES` | `4` | |
| `GMOD_MAX_RUNNING` | `2` | |
| `GMOD_TOTAL_BUDGET_GIB` | `16` | |

**These are seeded once.** They are read from the environment the first time
agrelha sees a game and then stored in SQLite; afterwards the stored values win
and the environment is never consulted again. Changing the variable later does
nothing — edit the settings in `/overview` instead.

## Storage and serving

| Key | Default |
|---|---|
| `DB_PATH` | `/data/agrelha.db` — the parent directory must exist |
| `LISTEN_ADDR` | `:8080` |
| `BACKUPS_DIR` | — a directory to report last-backup time and size from |

## Content sources and observability

| Key | Default |
|---|---|
| `MODRINTH_API` | `https://api.modrinth.com/v2` |
| `THUNDERSTORE_API` | `https://thunderstore.io/c/valheim/api/v1` |
| `CURSEFORGE_API` | `https://api.curseforge.com/v1` |
| `CURSEFORGE_API_KEY` | — CurseForge has no anonymous API; unset means it is simply absent |
| `STEAM_WEB_API_KEY` | — without it a Garry's Mod Workshop collection cannot be resolved |
| `INFLUXDB_URL` | — enables the metrics tiles |
| `GRAFANA_DASHBOARD_URL` | — adds a deep-link |
| `LOG_LEVEL` | `info` (`debug` traces SSE frames) |
| `LOG_FORMAT` | `text` (`text` or `json`) |

An **empty** API key is a supported state and degrades gracefully. A key that is
*missing* from a secret store which builds its target atomically is not — that
fails the whole secret and the container never starts.

## In-game administration over RCON (Minecraft)

| Key | Default |
|---|---|
| `MINECRAFT_RCON_ADDR` | `minecraft-modded.minecraft-modded.svc.cluster.local:25575` |
| `MINECRAFT_RCON_PASSWORD` | — RCON is disabled while unset |

## Paths inside the state repository

Defaults suit a repository laid out like this one; change them to match yours.

| Key | Default |
|---|---|
| `MODS_PATH` | `manifests/valheim/mods.yaml` |
| `ADMINS_PATH` | `manifests/valheim/admins.yaml` |
| `MINECRAFT_MODS_PATH` | `manifests/minecraft-modded/mods.yaml` |
| `MINECRAFT_ACCESS_PATH` | `manifests/minecraft-modded/access.yaml` |
| `MINECRAFT_CONFIGS_PATH` | `manifests/minecraft-modded/configs.yaml` |

Worlds on a Docker Host render to a separate tree instead, keyed by game and
number, so that whatever watches the Kubernetes paths never sees a Compose file
and tries to apply it.

## Provenance

| Key | Default |
|---|---|
| `SOURCE_URL` | the upstream repository |

agrelha's footer links here. If you modify agrelha and host it for other people,
AGPL section 13 obliges you to offer them the corresponding source — set this to
your own repository and the footer does it for you.
