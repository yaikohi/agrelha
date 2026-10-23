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
- [ ] **Every other secret still rides the same mangling path.** `CODEBERG_TOKEN`,
      `OIDC_CLIENT_SECRET`, `CURSEFORGE_API_KEY` and `CODEBERG_USERNAME` are still interpolated into
      the systemd `EnvironmentFile` on fogo. None contains a backslash today, so none is broken — but
      the next one that does will fail the same way, far from its cause. `config.envSecret` already
      supports `<VAR>_FILE` for all of them; moving them is a fogo-side change.
- [ ] **`TestBuildVariantsAndRoutes` is flaky.** Failed once in `internal/wiring`, then passed 9
      targeted runs and 2 full-suite runs. Not dismissed: with no CI, an intermittent failure is
      exactly how a real bug hides. Needs `-race` and a look at shared state between wiring tests.
- [ ] **Presence is single-world.** Found while fixing the ingester: `presenceStore` has no
      Instance key (`UpsertSeen(id, character, joined)`), so one stream serves the whole roster and
      the source resolves the lowest-numbered Valheim Instance. With three worlds the roster
      conflates them. Pre-existing, not a regression.

## Layering

- [ ] **`app` reads infra error strings.** `manager.go:704`:
      `strings.Contains(err.Error(), "No such container") || strings.Contains(err.Error(), "status 404")`.
      `docs/architecture.md`: *"`app` does not know `infra` exists… every external thing arrives as
      a port."* Fix with a sentinel (`ports.ErrServerAbsent`) returned by the Docker runtime and
      matched via `errors.Is`. Today a Docker release that rewords that string silently breaks
      convergence. ("container" is also on the **Instance** Avoid list; the same hunk uses `goto started`.)
- [ ] **Dead `Materializer` gate.** `manager.go:596` asserts
      `if mat, ok := m.reconciler.(ports.Materializer); ok` — always true, because
      `hostrouter/reconciler.go:68` declares `var _ ports.Materializer`. The real decision happens
      inside the router, which `return nil`s at line 63 for a non-Materializer, so "nothing was
      materialised" and "materialised fine" are the same answer.
- [ ] **`web` tests import infra adapters.** `handlers/gmod/handler_test.go:17-18` pulls in
      `infra/manifests/gmod` and `infra/store`, against *"web tests run against a Fiber app wired
      with fake `app` services"*.

## Vocabulary — three hard `CONTEXT.md` violations

Nothing machine-enforces `CONTEXT.md`, which is how all three landed.

- [ ] **The Access page is named `admins`.** `CONTEXT.md:252` — *"Avoid: admin (ambiguous with the
      agrelha operator)"*. Now in route (`/admins`), type (`AdminsProps`) and function
      (`renderAdminsPage`). Most invasive of the three: `/admins` is a live URL, and `admins`
      already means in-game Valheim operators, so this needs a decision about which concept keeps
      the word.
- [ ] **`Capabilities{ Modpacks bool }`** — `domain/game.go:36` + 3 uses, against `CONTEXT.md:111`
      *"Avoid: modpack (as a distinct concept)"*.
- [ ] **Lifecycle and Availability share one `State` field.** `gmod/game.go:78-95` sets
      `tele.State = "Up"` then `tele.State = string(st.Lifecycle)`, against `CONTEXT.md:237`
      *"never compared to each other and never share a field"* and *"Avoid: Up, Down"*. GMod copied
      an existing mistake — valheim and minecraft `game.go:163,169` do the same — so a real fix
      touches three games.

## Spec gaps

- [x] **`/custom-configs` declared** in Minecraft's `RuntimeSpec`, alongside the `config-sync`
      service that stages it into `/data/config` the way the cluster's init container does.
- [ ] **Data-root separation unenforced.** Spec: *"The data root must be a separate absolute path."*
      `compose/reconciler.go:38` documents it; nothing validates `DATA_DIR` against `COMPOSE_DIR`,
      so pointing them at the same path has the reconciler overwrite its own inputs.
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

- [ ] `hostrouter/{runtime,reconciler,renderer}.go` — `routes` map, `Hosts()`, `For()` written
      verbatim three times.
- [ ] The RAM-budget error string, and the whole `Telemetry` method, duplicated across all three
      dashboards.
- [ ] `instance-%02d` path shape in four places in `manager.go` plus `compose/reconciler.go`.
      `27015` is still a literal 5× in `helpers.go`; Minecraft's `25565` literals were retired via
      `pages.MinecraftPort`, and GMod can follow the same route now that `domain.HostPortFor` exists.
- [ ] `AdminsProps` carries 13 `Valheim*`/`MC*`/`GMod*` fields; `(host string, unreachable bool)`
      threads through `web/shared/convergence.go`, and `host string` where `domain.Host` exists.
- [ ] `admins.templ:51-62` hardcodes three games despite `domain.Profiles()`; `convergence.go`
      switches the same pair twice.
- [ ] `mcRedirectTarget(c *fiber.Ctx)` ignores its parameter and returns a constant, from 20 call
      sites that all pass `c`.
- [ ] `gmod/game.go:110` `ExportClientBundle` returns `domain.Bundle{}, nil`.
- [ ] `BuiltinResources` special-cases GMod then delegates to `LegacyResources`;
      `EnsureDefaults`/`Annotations` lost their `default: panic("unknown game")` while `EnvWith`
      kept it.
- [ ] `compose.EscapeVars` — name does not say what it escapes or why.

## Test-infrastructure debt

- [ ] **`mockStateStore` fidelity.** `Get` returns an empty document with a nil error where real git
      returns `fs.ErrNotExist`; `Patch` stores `Document.Data` while `PutTree` stores `Document.Raw`
      and they never round-trip. This is the direct cause of four false-green tests in one session.
      The pattern that fixes it: drive `InstanceManager` through the real `gitops.Committer` over
      go-git's filesystem transport, as `internal/infra/gitops/harness_test.go` already does.
- [ ] **No CI.** Nothing runs `go test` on push; every bug in this document reached the box first.
      agrelha also has no `flake.nix` — its `vendorHash` lives in the fogo repo.

## Smaller items

- [ ] `configs.yaml.tmpl` renders `namespace:` empty at the compose path (cosmetic — nothing applies
      that ConfigMap to Kubernetes for a Docker Host).
- [ ] `WithSourceReconciler` reads `BEPINEX` from a Deployment env var and is classified
      `cluster-only` in the ratchet ledger. Where a Docker-Host world's BepInEx version comes from is
      an open design question.
- [ ] Delete the `GetInstalledMods` legacy-path fallback. Now safe: it was load-bearing for
      instance-02 until its `compose/.../mods.yaml` was restored. Removing it closes the last route by
      which a stale `manifests/` file can answer a question about a Docker-Host world.
- [ ] Delete `manifests/valheim/instance-01` and `instance-02` once the restore is trusted — they are
      stale copies of live state, and currently also the only backup of the Overrides.
- [ ] `ResolveTree` still reaches the network during apply (latency, not correctness).
- [ ] fogo runs `-dirty via --local`; re-pin with `--bump` once agrelha and fogo are both committed.
      `/etc/nixos` on fogo is a stale second source of truth at `f486539`.
