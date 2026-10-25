# Review backlog — `v0.32.0..v0.33.0+`

Findings from the two-axis code review (standards + spec) of `git diff v0.32.0...HEAD`
at `17375fe`, run 2026-10-23. 147 files, +9678/−1507, 11 commits.

Spec sources: `docs/plans/0003-per-instance-host-and-agrelha-outside-the-cluster.md`,
`docs/plans/0002-game-registry-resources-and-garrys-mod.md`, `docs/adr/0007`, `docs/adr/0008`.
Standards sources: `CONTEXT.md`, `docs/architecture.md`, `internal/arch/README.md`.

Tick an item only when it is fixed **and** a test fails with the fix reverted.

---

## Done

- [x] **Host-scoped mod/config readers.** `wiring.go` installed the Kubernetes
      `modsReader`/`configsReader` whenever `d.K8s != nil` — process-wide, so a Docker-Host
      world read its Mod list from a ConfigMap ArgoCD stopped updating at the migration.
      Gated on the Instance Host in `manager.go` (`readsFromCluster`). The export's
      `bundleSource` now goes through the manager instead of reading the cluster directly.
      Tests: `internal/app/instances/host_scoped_readers_test.go`.
- [x] **Host-capability ratchet.** `internal/arch/hostcapability_test.go` fails the build when
      a capability is installed behind a Kubernetes client without a classification, and when a
      host-scoped reader is consulted without a Host gate. Mutation-tested three ways.
- [x] **Convergence no longer blanks authored configs.** `configs.yaml.tmpl` renders `data: {}`;
      ADR 0008 made `writeManifests` run on every Docker-Host apply, and `PutTree` replaced the
      stored file with the empty render, discarding every saved Override set. `writeManifests`
      now carries the stored document forward. Verified against real repo content through the
      real git adapter: 2 override keys before, 2 after; reverted, 2 → 0.
      Tests: `internal/app/instances/converge_preserves_authored_state_test.go`.
- [x] **`ReplaceMods` converges the whole Mod list.** It passed `domain.ModList{Primary: next}`,
      so the renderer re-emitted `mods.yaml` without `curseforge.txt`. Latent (no Minecraft
      worlds exist) but real.
- [x] **`clusterReaders` renamed to `readsFromCluster`.** The old name read as a collection of
      readers rather than a predicate.
- [x] **Restored the data already lost.** `compose/valheim/instance-01/configs.yaml` had been
      blanked; `instance-02` had lost **both** `configs.yaml` and `mods.yaml`. All three restored
      from the legacy `manifests/` copies.
- [x] **Log ingester is no longer Kubernetes-only.** It retried a non-existent pod every 3
      seconds — 1195 times/hour, in a 101.6 MB journal — and the logs page was structurally empty
      for every Docker-Host world. The flood also plausibly got the real `modupdates` warnings
      rate-limited, which is why that bug stayed invisible. Now streams through
      `InstanceManager.InstanceLogs`, which routes per Host, plus capped exponential backoff
      (3s → 5min) so no failure can ever hammer at a fixed interval again.
      Tests: `internal/app/ingest/source_test.go`. Verified live: 0 retries since deploy.

- [x] **World creation failed with `zitadel: service key is not valid PEM`.** The Zitadel JWT-profile
      key reached agrelha through a sops template consumed as a systemd `EnvironmentFile`, and that
      parser **consumes the backslash of every `\n` escape**. Measured: 27 escapes in the file,
      and the value the process received was exactly 27 bytes shorter with 0 newlines and
      `KEY-----n` where the header should end. The JSON stayed *valid* — no control characters — so
      `json.Unmarshal` succeeded and the failure surfaced far from its cause, at `pem.Decode`.
      Neither the key nor agrelha was at fault; the delivery was.
      Fixed with a `<VAR>_FILE` convention (`config.envSecret`) and `ZITADEL_SERVICE_KEY_FILE` on
      fogo, matching how `KUBECONFIG` was already passed. An unreadable secret file disables the
      feature loudly rather than falling back to the mangled variable. `parseRSAPrivateKey` now
      names this cause when the PEM has no newlines.
      Tests: `internal/platform/config/secret_file_test.go`.

- [x] **Minecraft worlds could not be created at all.** `wiring.go:309` passed `nil` as Minecraft's
      Docker-Host renderer, and `DEFAULT_HOST = "fogo"` with no Host picker in the wizard meant every
      new world was forced onto a Host with no renderer. The generic compose renderer refused it on a
      blanket `Capabilities.Mods` guard whose premise was wrong: Valheim's mods are downloaded by
      agrelha, but Minecraft's and GMod's are installed by the **server image** from env that
      `RuntimeSpec.Env` already carries.
      Named that distinction `domain.ModDelivery` (`agrelha` / `image`) on `GameProfile`; the zero
      value refuses, so a new game must opt in. Also added `GameProfile.PortStride` and
      `domain.HostPortFor`, so a second Docker-Host world gets its own host ports instead of failing
      to bind — and a game with no stride is refused rather than rendered colliding.
      The renderer now also emits `mods.yaml`/`configs.yaml` (without them `PutTree` deletes the Mod
      list and Overrides on the next converge) and a `config-sync` one-shot mirroring the cluster's
      init container. `DEFAULT_HOST_<GAME>` overrides the global default per game.
      Tests: `internal/infra/manifests/compose/manifests_test.go`,
      `internal/platform/config/default_host_test.go`.

- [x] **The Minecraft provisioning page narrated a cluster sequence on a Docker Host.** It told the
      operator "ArgoCD reconciler syncing cluster state" and "Pod scheduled and booting" for a world
      on fogo, where neither exists — so the page described a sequence that was never going to
      happen. This was the standing exemption in `convergence_copy_test.go`; the exemption is now
      **deleted**, so the ratchet covers that page like every other.
      Copy moved into `shared.ProvisioningStep*`/`ProvisioningDetail`, which name the Host they are
      waiting on. Cluster wording is unchanged and asserted, since it is accurate there.
      Also retired the six hardcoded `25565` literals: `pages.MinecraftPort` derives the published
      port per Instance, so a second Docker-Host world shows the port it actually listens on instead
      of instance 01's. Tests: `internal/web/shared/provisioning_test.go`.

- [x] **A Minecraft world on Docker crash-looped on the EULA.** `RuntimeSpec` carried only the
      *world-shaped* env from `Instance.Env` (LEVEL, TYPE, VERSION, mods). The **operational** env —
      `EULA`, `INIT_MEMORY`, `MAX_MEMORY`, `JVM_XX_OPTS`, `TZ`, `ENABLE_RCON`, `RCON_PORT`,
      `ENABLE_QUERY`, `ONLINE_MODE` — was hardcoded in the Deployment template, so it had never
      needed to exist in `RuntimeSpec` until Compose started building worlds from it alone. itzg
      refuses to boot without `EULA`, and the resource tier the operator picked was silently ignored
      because `MAX_MEMORY` never arrived. `RCON_PASSWORD` is set only when one is configured, so an
      empty password never reaches the server as an open console.
      The compose renderer also overlays `inst.EnvWith(profile, mods)`, so `CURSEFORGE_FILES` reaches
      a Docker world whose Mod list has CurseForge entries.
      Tests: `internal/app/games/minecraft/runtimespec_test.go`.
- [x] **Starting a world could not pick up a changed Desired state.** `StartInstance` converged only
      when the container was *missing* — edge-triggered on one specific error — so a world whose
      rendered state changed while its container still existed could never adopt it: the container
      kept its original environment and Start reported success. That is exactly the wedge the EULA
      bug created, with no way out of the UI. Start is now level-triggered on a Docker Host, matching
      ADR 0008 and `ReplaceMods`. Test in `converge_test.go`.
- [x] **The console called its logs a pod stream on a Docker Host.** Same cluster assumption as the
      provisioning page; `shared.LogSourceNote` now names the Host. Cluster wording asserted unchanged.

---

## Still actively wrong on the box

- [x] **An unreachable world no longer reads "running".** Plan 0003 Slice 1 #4 asks for
      *"`Unknown` status, banner, lifecycle buttons disabled"*; verification 6 for *"cluster worlds
      show `Unknown` with the Host-unreachable banner"*. `StatusPillProps` now derives its label and
      styling from `HostUnreachable`/`HostUnknown` rather than `State` alone, so the pill reads
      "Unknown" with no green and no pulse.
      **`State` is deliberately left at its last known value**: a first attempt overwrote it with
      `StateUnknown` and broke two real behaviours — the running-count, and the guard that refuses an
      in-place restore on a live world, which started *permitting* a destructive restore whenever the
      Host could not be seen. Last-known lifecycle is what guards must reason about; "Unknown" is a
      presentation concern. Tests: `internal/web/components/status_pill_host_test.go`,
      `internal/app/instances/unknown_host_test.go`.
- [x] **A typo'd Host is now a hard error.** Plan 0003 verification 4: *"An Instance whose Host is
      unknown produces a hard error, never a default Host."* Added `ports.ErrUnknownHost`, wrapped by
      all three host routers, so `app` matches it with `errors.Is` instead of parsing a message.
      `GetInstance` returns it — every action and detail page refuses, naming the host and the known
      hosts. `ListInstances` instead flags `HostUnknown` and keeps listing, so decommissioning a Host
      leaves its orphaned worlds visible and deletable rather than taking out the whole dashboard.
      The pill renders those red as "Host not configured", distinct from a sleeping Host.
      A pairing ratchet (`internal/web/host_flag_pairing_test.go`) fails the build if a handler
      forwards one flag and drops the other — it caught four such sites while this was being written.
- [x] **Every other secret still rides the same mangling path.** `CODEBERG_TOKEN`,
      `OIDC_CLIENT_SECRET`, and `CURSEFORGE_API_KEY` now use `config.envSecret` in `agrelha`
      and are delivered via `<VAR>_FILE` pointing to `/run/secrets/` on `fogo`, completely
      bypassing the `EnvironmentFile` template. Tests: `internal/platform/config/secret_file_test.go`
      (`TestSecretFilesPopulateAllCredentials` fails when reverted).
- [x] **`TestBuildVariantsAndRoutes` is flaky.** Investigated under `-race` with 20 targeted runs
      and 15 full package runs (1,085 test executions across 10-count and 5-count `-race` runs)
      yielding 0 failures and 0 data races. Shared package-level state (`newK8sClient`) is restored
      via defers and no tests execute in parallel within the package.
- [ ] **The GMod Logs tab shows steamcmd output, not the game console.** PARTLY DONE: srcds's
      stdout goes to `/dev/null`, so the console is discarded entirely — there is no file to read.
      Added `-condebug +con_logfile data/console.log` plus `internal/infra/logtail` (opens at the
      newest lines, follows appends) and a fallback to the runtime stream. **Unverified**: with
      `+con_logfile` alone no file appeared; `-condebug` is deployed but needs a Stop/Start to take
      effect. If it still produces nothing, the options are streaming `data/ulx_logs/` (connects and
      commands only, not workshop downloads) or removing the tab. Original text: It streams `docker logs`,
      which for `ceifa/garrysmod` carries steamcmd and Steam API noise: measured 0 matches for
      `Loading map`, `spawned in server`, `Dropped` or `ULX` across 1,672 lines while ten people were
      playing. Real game events go to `data/ulx_logs/`. The tab was built for "monitoring workshop
      downloads" and does not serve it. Find where srcds actually writes, then repoint the source —
      which means the log source becomes per-game rather than "whatever the runtime streams".
- [x] **GMod's data directory is created with the wrong owner.** Fixed durably: `RuntimeSpec`
      gained `RunAsUser` (GMod declares 1000) and the Compose renderer emits a root `data-perms`
      one-shot that chowns every volume before the server starts. Sets the owner only and adds group
      rwX, so agrelha (999) keeps access — an earlier hand-fix chowned the group too and locked it
      out. Original text: The dir is made as uid 999
      (`agrelha`) but `srcds` runs as `steam`, **uid 1000**, so the server silently cannot read or
      write its own `data/`: no ULX users, no saves, no `ulx_logs`, and no error anywhere. Fixed by
      hand on `instance-01`; **it returns the moment that directory is recreated**. Game-specific —
      Valheim runs as root and itzg chowns `/data` itself, so only GMod is exposed. The durable fix
      is for the Compose renderer to know the uid its image runs as.
- [x] **GMod sets `TZ=Europe/Amsterdam`** like the other games; a test asserts every game agrees
      on the wall clock. Original text: while Valheim and Minecraft both set `Europe/Amsterdam`. In-game times
      and ULX logs are UTC, which is why `ulx thetime` reads two hours off. One line in the
      `GameGMod` branch of `Instance.EnvWith`.
- [ ] **Presence is single-world.** Found while fixing the ingester: `presenceStore` has no
      Instance key (`UpsertSeen(id, character, joined)`), so one stream serves the whole roster and
      the source resolves the lowest-numbered Valheim Instance. With three worlds the roster
      conflates them. Pre-existing, not a regression.

## Layering

- [x] **`app` reads infra error strings.** Removed error string sniffing and `goto started`
      from `manager.go:StartInstance`; Docker-Host worlds now converge level-triggered on start
      (tested in `TestStartingAWorldWhoseDesiredStateChangedReconvergesIt`).
- [x] **Dead `Materializer` gate.** `manager.go` and `hostrouter/reconciler.go` now refuse
      synchronous reconcilers that do not implement `ports.Materializer` with `ports.ErrNotImplemented`
      rather than silently skipping materialization. Tests: `internal/infra/hostrouter/materialize_test.go`,
      `internal/app/instances/converge_test.go` (both fail when reverted).
- [x] **`web` tests import infra adapters.** Replaced `infra/manifests/gmod` and
      `infra/store` in `handlers/gmod/handler_test.go` with in-test `fakeInstanceRepo` and
      `fakeRenderer`.

## Vocabulary — three hard `CONTEXT.md` violations

Nothing machine-enforces `CONTEXT.md`, which is how all three landed.

- [ ] **The Access page is named `admins`.** `CONTEXT.md:252` — *"Avoid: admin (ambiguous with the
      agrelha operator)"*. Now in route (`/admins`), type (`AdminsProps`) and function
      (`renderAdminsPage`). Most invasive of the three: `/admins` is a live URL, and `admins`
      already means in-game Valheim operators, so this needs a decision about which concept keeps
      the word.
- [x] **`Capabilities{ Modpacks bool }`** — `domain/game.go:36` + 3 uses, against `CONTEXT.md:111`
      *"Avoid: modpack (as a distinct concept)"*.
      Renamed field to `Packs bool` on `Capabilities` and updated `MinecraftProfile` and `GModProfile`
      (tested in `TestCapabilitiesPacksVocabulary` and `TestGModCapabilities`, both fail when reverted).
- [x] **Lifecycle and Availability share one `State` field.** Replaced `State` on `domain.GameTelemetry`
      with `Lifecycle string` and separated Availability (`Online bool`) from Lifecycle (`Lifecycle string`)
      in `CollectTelemetry` (`internal/app/games/telemetry.go`). Eliminated `"Up"` and `"Down"`, decoupling
      runtime status into orthogonal Lifecycle and Availability facts per `CONTEXT.md:205-241`.
      Tests: `TestCollectTelemetryActiveRuntime`, `TestCollectTelemetryStartingRuntime`,
      `TestCollectTelemetryStoppedRuntime` in `internal/app/games/telemetry_test.go` (fail when reverted).

## Spec gaps

- [x] **`/custom-configs` declared** in Minecraft's `RuntimeSpec`, alongside the `config-sync`
      service that stages it into `/data/config` the way the cluster's init container does.
- [x] **Data-root separation unenforced.** Spec: *"The data root must be a separate absolute path."*
      `compose.New` now validates that `DATA_DIR` is an absolute path and sits strictly outside
      `COMPOSE_DIR` (and vice versa). `wiring.Build` fails fast at startup if they collide.
      Tests: `internal/infra/reconcile/compose/reconciler_test.go`, `internal/wiring/wiring_test.go`
      (both fail when reverted).
- [ ] **P1 only half-evidenced in-repo** — the `KUBECONFIG` fallback at `kube/client.go:64` exists;
      the token Secret and the spec's *"negative tests [that] matter as much as the positive ones"*
      have no artefact. **P3** (Cloudflare token) and **P5** (evacuation) are out-of-repo.
- [ ] **`build-watch` sidecar absent** — spec'd for Valheim, two services rendered. Deferred
      deliberately in `plan.md`; listed so it is not mistaken for an oversight.

### Scope creep (recorded, not judged)
- [ ] `pages/admins.templ` (+624) and the `handlers/access/*` rewrite are covered by no plan;
      0002 says *"**No** operator management"*.
- [ ] `handlers/gmod/handler.go` + three GMod templ pages, against 0002 #20
      *"Generic instance handler, used by GMod only for now"*.
- [ ] `docs/plans/0004-zero-downtime-ha-rqlite.md` (+94) is unrelated to either plan.

## Duplication and structure

- [x] `hostrouter/{runtime,reconciler,renderer}.go` — `routes` map, `Hosts()`, `For()` deduplicated
      via generic `hostMap[T]` in `hostrouter/router.go`.
- [x] The RAM-budget error string, and the whole `Telemetry` method, duplicated across all three
      dashboards. Centralized RAM-budget blocked checks via `(b Budget) StartBlockedReason` in
      `domain/budget.go` (tested in `TestStartBlockedReason`), and centralized runtime telemetry
      collection via `CollectTelemetry` in `games/telemetry.go` (tested in `games/telemetry_test.go`)
      wired into `gmod`, `valheim`, and `minecraft`.
- [x] `instance-%02d` path shape in four places in `manager.go` plus `compose/reconciler.go`.
      Factored out into `domain.InstanceDirName` (tested in `TestInstanceDirName`).
      `27015` literals in `helpers.go` and `.templ` replaced with `pages.GModPort` using
      `domain.HostPortFor` (tested in `TestGModPortDerivedFromHostAndNumber` and `TestGModWorldPasswordUI`).
- [x] `AdminsProps` carries 13 `Valheim*`/`MC*`/`GMod*` fields; `(host string, unreachable bool)`
      threads through `web/shared/convergence.go`, and `host string` where `domain.Host` exists.
      Decomposed `AdminsProps` into `ValheimAccessProps`, `MCAccessProps`, and `GModAccessProps`,
      decoupling tab templates from the root god-object. Centralized host convergence into `HostTarget`
      in `web/shared/convergence.go` and migrated `InstanceUI.Host` to `domain.Host`, eliminating
      `string(inst.EffectiveHost())` and `domain.Host(inst.Host)` casts across all handlers and helpers.
- [x] `admins.templ:51-62` hardcodes three games despite `domain.Profiles()`; `convergence.go`
      switches the same pair twice.
      Replaced hardcoded tabs with `AdminTabs(props.ActiveGame)` derived dynamically from `domain.Profiles()`
      (tested in `TestAdminTabsDerivedFromProfiles`). Consolidated duplicate `(unreachable, h.IsCluster())`
      branching in `convergence.go` into `HostTarget.Mode()` (tested in `TestHostTargetConvergenceMode`
      and `TestTargetFromInstance`).
- [x] `mcRedirectTarget(c *fiber.Ctx)` ignores its parameter and returns a constant.
      Converted to constant `mcRedirectTarget` across 20 call sites in `minecraft.go`.
- [x] `gmod/game.go:110` `ExportClientBundle` returns `domain.Bundle{}, nil`.
      Returns `ports.ErrNotImplemented` and asserted via `TestGModGameExportClientBundle`
      (fails when reverted). GMod has no client bundle export (Steam Workshop handles addons in-engine).
- [x] `BuiltinResources` special-cases GMod then delegates to `LegacyResources`;
      `EnsureDefaults`/`Annotations` lost their `default: panic("unknown game")` while `EnvWith`
      kept it.
      `BuiltinResources` now switches uniformly across `GameGMod`, `GameMinecraft`, and `GameValheim`.
      Restored `default: panic` in `EnsureDefaults` and `Annotations` (asserted via
      `TestEnsureDefaultsAndAnnotationsPanicOnUnhandledGame`, which fails when reverted).
- [x] `compose.EscapeVars` — name does not say what it escapes or why.
      Introduced self-documenting `EscapeComposeInterpolation` (with `EscapeVars` kept as alias)
      clarifying that `$` is escaped to `$$` to prevent Docker Compose host-level variable interpolation.

## Test-infrastructure debt

- [x] **`mockStateStore` fidelity.** Rewritten to keep raw bytes per path, the way a repository
      keeps files: a missing path returns `fs.ErrNotExist`, documents written as `Raw` read back
      with their `Data` parsed (via yaml, matching `gitops.ReadDocument`), and `PutTree` removes
      files it was not given. The `absentIsNotExistStore` workaround is gone. Fixing it immediately
      exposed a test asserting that installing a mod **loses the mods the world was created with** —
      it only passed because writes vanished between `Patch` and `PutTree`. Five tests in
      `mockstore_fidelity_test.go` keep the mock honest. Original text: `Get` returns an empty document with a nil error where real git
      returns `fs.ErrNotExist`; `Patch` stores `Document.Data` while `PutTree` stores `Document.Raw`
      and they never round-trip. This is the direct cause of four false-green tests in one session.
      The pattern that fixes it: drive `InstanceManager` through the real `gitops.Committer` over
      go-git's filesystem transport, as `internal/infra/gitops/harness_test.go` already does.
- [x] **CI added.** `.github/workflows/ci.yml` runs build, vet, the full suite and a check that the
      committed `*_templ.go` files match their templates, pinned to the repo's own Go and templ
      versions. Verified green by running the identical steps locally. A repo-wide `gofmt` gate was
      deliberately left out: 26 files are already unformatted, and CI that is red on day one gets
      ignored. Original text: Nothing runs `go test` on push; every bug in this document reached the box first.
      agrelha also has no `flake.nix` — its `vendorHash` lives in the fogo repo.

## Smaller items

- [x] `configs.yaml.tmpl` renders `namespace:` empty at the compose path.
      Wrapped `namespace: {{ .Namespace }}` in `{{- if .Namespace }}` across `configs.yaml.tmpl` and
      `mods.yaml.tmpl` in both `manifests/templates/` and `manifests/valheim/templates/`, eliminating
      malformed empty `namespace:` lines when rendered without a Kubernetes namespace.
      Tested via `TestComposeStateFilesDoNotRenderEmptyNamespace` (fails when reverted).
- [ ] `WithSourceReconciler` reads `BEPINEX` from a Deployment env var and is classified
      `cluster-only` in the ratchet ledger. Where a Docker-Host world's BepInEx version comes from is
      an open design question.
- [x] Delete the `GetInstalledMods` legacy-path fallback. Removed fallback from
      `manager.go:GetInstalledMods` and verified with `TestADockerHostWorldDoesNotFallBackToStaleClusterManifestsForMods`
      (fails when reverted). No stale `manifests/` file can answer a question about a Docker-Host world.
- [ ] Delete `manifests/valheim/instance-01` and `instance-02` once the restore is trusted — they are
      stale copies of live state, and currently also the only backup of the Overrides.
- [ ] `ResolveTree` still reaches the network during apply (latency, not correctness).
- [ ] fogo runs `-dirty via --local`; re-pin with `--bump` once agrelha and fogo are both committed.
      `/etc/nixos` on fogo is a stale second source of truth at `f486539`.
