# Ponytail Audit Fixes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remediate all 14 findings from the Ponytail audit (goroutine leaks, insecure default auth key, log replay, slug corruption, budget races, RCON injection, and dead code) while strictly honoring the project's Domain-Driven Design (DDD) and hexagonal architecture constraints.

**Architecture:** The core (`domain` -> `ports` -> `app`) remains pure with zero framework/database dependencies. HTTP handlers (`web`) consume application use-cases via `ports`/`app` with request-scoped contexts (`c.UserContext()`). Infrastructure adapters (`infra`) handle I/O and protocol specifics (SQLite pooling, RCON client lifecycle, HTTP dial control) without leaking into the domain or driving layers. Configuration changes flow through `platform/config` and are bound exclusively in `wiring`.

**Tech Stack:** Go 1.26, Fiber v2, SQLite (`modernc.org/sqlite`), Kubernetes `client-go`, RCON, Docker/Compose, Templ.

**Spec:** Ponytail Audit Report (Findings 1–14).

## Global Constraints

- Never breach layer dependencies: `domain` imports nothing; `ports` imports only `domain`; `app` and `infra` import only `domain`, `ports`, `platform`; `web` imports `domain`, `ports`, `platform`, `app`, `web`; `config` is imported ONLY by `wiring` and `cmd/`.
- All shell and build commands must be prefixed with `rtk` (e.g. `rtk go test ./...`, `rtk task test`).
- Every handler or function must be read first, and callers grepped before modification.
- Test-Driven Development (TDD): Write a failing unit test first, verify failure, write minimal fix, verify pass.

## Review Focus

1. **Client disconnect during active SSE streaming:** Ensure goroutines and underlying Kubernetes pod log readers terminate promptly upon TCP connection loss.
2. **Empty or missing local auth secret:** Ensure unconfigured local auth does NOT fall back to a known static secret, preventing forged admin cookies.
3. **Hyphenated modpack archive jar names:** Ensure mod names with hyphens (e.g., `fabric-api-0.92.0.jar`) parse to `fabric-api` rather than being truncated to `fabric`.
4. **Concurrent instance creation and startup:** Ensure parallel requests cannot bypass memory budget limits and overcommit node RAM.
5. **Special characters in Minecraft usernames:** Ensure newlines, semicolons, and spaces in player usernames cannot inject secondary commands into RCON or corrupt GitOps ConfigMaps.

---

### Task 1: Fix Insecure Default Secret Key in Local Auth (Finding 3)

**Files:**
- Modify: `internal/infra/auth/local/local.go:108-122`
- Modify: `internal/platform/config/config.go:65-75, 140-150`
- Modify: `internal/wiring/wiring.go:1180-1188`
- Test: `internal/infra/auth/local/local_test.go`

**Interfaces:**
- Consumes: `ports.UserStore`
- Produces: `local.New(store ports.UserStore, secretKey string, opts ...Option) *Authenticator` (ephemeral random 32-byte secret generated if empty, eliminating the hardcoded default key).

- [ ] **Step 1: Write failing unit test in `local_test.go`**
  Assert that when `secretKey` is empty, two separate `local.New()` instances generate distinct random keys and cannot validate each other's cookies (verifying the hardcoded static secret is removed).

- [ ] **Step 2: Run test to verify it fails**
  Run: `rtk go test -v ./internal/infra/auth/local -run TestNew_EmptySecretRandom`
  Expected: FAIL (currently both use the shared hardcoded string `"agrelha-local-auth-secret-key-v1"`).

- [ ] **Step 3: Implement ephemeral random key generation and config field**
  1. Add `LocalAuthSecret string` to `platform/config/config.go` mapping to `LOCAL_AUTH_SECRET`.
  2. In `local.New`: if `secretKey == ""`, generate a cryptographically secure 32-byte random key via `crypto/rand`.
  3. In `wiring.go`: pass `cfg.LocalAuthSecret` (fallback to `cfg.OIDCClientSecret`) into `local.New`.

- [ ] **Step 4: Run test to verify it passes**
  Run: `rtk go test -v ./internal/infra/auth/local -run TestNew_EmptySecretRandom`
  Expected: PASS.

- [ ] **Step 5: Verify architecture compliance**
  Run: `rtk go test -v ./internal/arch`
  Expected: PASS.

---

### Task 2: Validate Minecraft Usernames to Prevent RCON & GitOps Injection (Finding 7)

**Files:**
- Create/Modify: `internal/domain/user.go` or `internal/app/access/validate.go`
- Modify: `internal/app/access/access.go:120-135, 230-245`
- Modify: `internal/web/handlers/access/minecraft.go:39-48`
- Test: `internal/app/access/access_test.go`
- Test: `internal/web/handlers/access/minecraft_test.go`

**Interfaces:**
- Produces: `domain.ValidateMinecraftUsername(username string) error`
- Consumes: Used in `access.AccessService.GrantOp`, `RevokeOp`, `AddWhitelist`, `RemoveWhitelist`, and Fiber handler `MCAccessGrantOp`.

- [ ] **Step 1: Write failing unit tests for username validation**
  Add test cases in `access_test.go` with invalid usernames (`steve\n/op evil`, `player;rm`, `too_long_username_exceeding_sixteen_chars`, `invalid name`).

- [ ] **Step 2: Run test to verify failure**
  Run: `rtk go test -v ./internal/app/access -run TestUsernameValidation`
  Expected: FAIL.

- [ ] **Step 3: Implement `ValidateMinecraftUsername` and check in access service**
  1. Define `var mcUsernameRegex = regexp.MustCompile("^[a-zA-Z0-9_]{1,16}$")`.
  2. Check `ValidateMinecraftUsername` at the entry point of `access.GrantOp`, `access.AddWhitelist`, etc., returning an error before calling GitOps or RCON.
  3. In `internal/web/handlers/access/minecraft.go`, return 400 Bad Request if username validation fails.

- [ ] **Step 4: Run test to verify it passes**
  Run: `rtk go test -v ./internal/app/access ./internal/web/handlers/access`
  Expected: PASS.

---

### Task 3: Fix Container Log Stream Goroutine Leaks & Unify Log Streamer (Findings 1, 13, 14)

**Files:**
- Modify: `internal/web/handlers/console/handler.go`
- Modify: `internal/web/handlers/console/minecraft.go:80-105`
- Modify: `internal/web/handlers/console/valheim.go:28-84, 120-150`
- Modify: `internal/web/handlers/console/gmod.go:28-50`
- Test: `internal/web/handlers/console/console_test.go`

**Interfaces:**
- Produces: `(h *Handler) streamLogs(c *fiber.Ctx, stream io.ReadCloser, selector string) error`
- Consumes: `ports.Runtime.Logs`, `InstanceManager.InstanceLogs` with `c.UserContext()`.

- [ ] **Step 1: Write failing test for client-disconnect and error exit in `console_test.go`**
  Simulate a cancelled context or closed response body writer and verify `streamLogs` exits promptly without blocking on the scanner.

- [ ] **Step 2: Run test to verify failure**
  Run: `rtk go test -v ./internal/web/handlers/console -run TestStreamLogs_Disconnect`
  Expected: FAIL.

- [ ] **Step 3: Implement unified `streamLogs` helper and delete dead `SSELogs`**
  1. In `console/handler.go`, implement `streamLogs(c *fiber.Ctx, stream io.ReadCloser, selector string) error`.
     - Defer `stream.Close()`.
     - In `SetBodyStreamWriter`, scan lines. If `sse.AppendElement(w, selector, frag)` returns an error, immediately `return`.
  2. In `minecraft.go`, `valheim.go`, and `gmod.go`:
     - Pass `c.UserContext()` to `InstanceLogs` and `gmodStream`.
     - Delegate the streaming loop to `h.streamLogs(...)`.
  3. Delete obsolete `SSELogs` from `valheim.go` and remove its unreferenced route from `console/handler.go`.

- [ ] **Step 4: Run test to verify it passes**
  Run: `rtk go test -v ./internal/web/handlers/console`
  Expected: PASS.

---

### Task 4: Fix Runaway Polling Loop in Wizard Provisioning Stream (Finding 2)

**Files:**
- Modify: `internal/web/handlers/wizard/wizard.go:578-615`
- Test: `internal/web/handlers/wizard/wizard_test.go`

**Interfaces:**
- Consumes: `c.UserContext()`, `sse.PatchSignals`

- [ ] **Step 1: Write unit test simulating client disconnect during provisioning**
  Test that `MCProvisioningStream` aborts when the client connection terminates or context cancels.

- [ ] **Step 2: Run test to verify failure**
  Run: `rtk go test -v ./internal/web/handlers/wizard -run TestProvisioningStream_ContextDone`
  Expected: FAIL.

- [ ] **Step 3: Implement early exit on write failure or context cancellation**
  1. Pass `c.UserContext()` into `provisioningStatus`.
  2. In `emit(...)`, check error from `sse.PatchSignals(w, ...)`. If non-nil, return `false`.
  3. If `emit` fails or `c.UserContext().Err() != nil`, immediately exit the polling loop.

- [ ] **Step 4: Run test to verify it passes**
  Run: `rtk go test -v ./internal/web/handlers/wizard`
  Expected: PASS.

---

### Task 5: Prevent Log Replay from Inflating Player Sessions on Reconnect (Finding 4)

**Files:**
- Modify: `internal/app/ingest/ingest.go:76-105`
- Test: `internal/app/ingest/ingest_test.go`

**Interfaces:**
- Consumes: `logStreamer.StreamLogs(ctx, 200)`
- Produces: Timestamp/session-guarded `consume(ctx context.Context, instance int, k logStreamer, st presenceStore) error`.

- [ ] **Step 1: Write unit test simulating repeated stream reconnects**
  Verify that calling `consume` multiple times with identical log history does not increment session count repeatedly for the same ongoing session.

- [ ] **Step 2: Run test to verify failure**
  Run: `rtk go test -v ./internal/app/ingest -run TestConsume_NoReplayDuplicateSessions`
  Expected: FAIL.

- [ ] **Step 3: Implement connection deduplication in `consume`**
  Only increment sessions and emit `join` events if the player was not already recorded as online, or if the event occurred after the ingestion stream attached.

- [ ] **Step 4: Run test to verify it passes**
  Run: `rtk go test -v ./internal/app/ingest`
  Expected: PASS.

---

### Task 6: Mutex-Guard Instance Creation & Start Budget Checks (Finding 6)

**Files:**
- Modify: `internal/app/instances/manager.go:560-600, 815-850`
- Test: `internal/app/instances/manager_test.go`

**Interfaces:**
- Consumes: `domain.CalculateBudget`
- Produces: Mutex synchronization protecting slot number allocation and memory budget reservation in `InstanceManager`.

- [ ] **Step 1: Write concurrent creation test in `manager_test.go`**
  Launch 4 goroutines calling `CreateInstance` simultaneously when budget only permits 2 instances; verify exactly 2 succeed and 2 receive budget errors without race conditions.

- [ ] **Step 2: Run test to verify race or failure**
  Run: `rtk go test -race -v ./internal/app/instances -run TestCreateInstance_ConcurrentBudget`
  Expected: FAIL or race detected.

- [ ] **Step 3: Implement concurrency lock in `InstanceManager`**
  Add `budgetMu sync.Mutex` to `InstanceManager` to serialize budget calculation, instance number allocation, and initial manifest commitment.

- [ ] **Step 4: Run test to verify it passes cleanly with `-race`**
  Run: `rtk go test -race -v ./internal/app/instances -run TestCreateInstance_ConcurrentBudget`
  Expected: PASS.

---

### Task 7: Fix Mod Slug Truncation on Hyphenated Mod Names (Finding 5)

**Files:**
- Modify: `internal/app/modpack/import.go:67-79, 148-163`
- Test: `internal/app/modpack/import_test.go`

**Interfaces:**
- Produces: `cleanModSlug(filename string) string`

- [ ] **Step 1: Write failing unit test for hyphenated mod names in `import_test.go`**
  Test inputs: `fabric-api-0.92.0.jar`, `better-end-4.0.jar`, `cloth-config-13.0.121.jar`, `jei-1.21.1.jar`.
  Expected outputs: `fabric-api`, `better-end`, `cloth-config`, `jei`.

- [ ] **Step 2: Run test to verify failure**
  Run: `rtk go test -v ./internal/app/modpack -run TestCleanModSlug`
  Expected: FAIL (currently outputs `fabric`, `better`, `cloth`).

- [ ] **Step 3: Implement `cleanModSlug` regex version stripper**
  Use regex matching `-[0-9]` to find where version strings begin rather than splitting on the first hyphen. Use `cleanModSlug` in both `ParseMrpack` and `ParsePrismZip`.

- [ ] **Step 4: Run test to verify it passes**
  Run: `rtk go test -v ./internal/app/modpack -run TestCleanModSlug`
  Expected: PASS.

---

### Task 8: Increase Fiber BodyLimit for Large Modpack Archives (Finding 9)

**Files:**
- Modify: `internal/web/routes.go:134-142`
- Test: `internal/web/routes_test.go` or `internal/web/handlers/wizard/wizard_test.go`

**Interfaces:**
- Consumes: `fiber.Config`

- [ ] **Step 1: Write test verifying a 10MB payload is accepted by Fiber**
  Send a 10MB multipart POST request to `/api/minecraft/wizard/import` and check that it does not return HTTP 413.

- [ ] **Step 2: Run test to verify failure**
  Run: `rtk go test -v ./internal/web/handlers/wizard -run TestMCWizardImport_PayloadSize`
  Expected: FAIL (Fiber rejects bodies > 4MB with 413).

- [ ] **Step 3: Configure `BodyLimit: 64 * 1024 * 1024` in `routes.go`**
  Set `BodyLimit: 64 * 1024 * 1024` in `fiber.Config` in `internal/web/routes.go:135`.

- [ ] **Step 4: Run test to verify it passes**
  Run: `rtk go test -v ./internal/web/handlers/wizard -run TestMCWizardImport_PayloadSize`
  Expected: PASS.

---

### Task 9: Propagate Server Start Errors in World Creation Wizards (Finding 8)

**Files:**
- Modify: `internal/web/handlers/wizard/wizard.go:498-506`
- Modify: `internal/web/handlers/valheim/wizard.go:614-622`
- Test: `internal/web/handlers/wizard/wizard_test.go`
- Test: `internal/web/handlers/valheim/handlers_edge_test.go`

**Interfaces:**
- Consumes: `StartInstance` return error

- [ ] **Step 1: Write unit test simulating `StartInstance` error during world creation**
  Verify that when `StartInstance` returns an error, the response flash or toast warns the user that instance creation succeeded but start failed.

- [ ] **Step 2: Run test to verify failure**
  Run: `rtk go test -v ./internal/web/handlers/wizard -run TestCreateWorld_StartInstanceError`
  Expected: FAIL.

- [ ] **Step 3: Handle error in `wizard.go` and `valheim/wizard.go`**
  If `err := h.cfg.MCInstances.StartInstance(...)` returns an error, log it and set a warning flash: `"World created, but failed to start automatically: " + err.Error()`.

- [ ] **Step 4: Run test to verify it passes**
  Run: `rtk go test -v ./internal/web/handlers/wizard ./internal/web/handlers/valheim`
  Expected: PASS.

---

### Task 10: TOCTOU DNS Rebinding Defense in Image Proxy (Finding 10)

**Files:**
- Modify: `internal/web/handlers/content/handler.go:88-144`
- Test: `internal/web/handlers/content/handler_test.go`

**Interfaces:**
- Consumes: `net.Dialer.Control`

- [ ] **Step 1: Write test verifying proxy blocks connection when DNS resolves to a private IP on dial**
  Add test asserting that dialing a host that resolves to loopback/private IP fails at connection time.

- [ ] **Step 2: Run test to verify failure**
  Run: `rtk go test -v ./internal/web/handlers/content -run TestImageProxy_PrivateIPDial`
  Expected: FAIL.

- [ ] **Step 3: Add socket-level IP filter in HTTP transport `DialContext`**
  In `imgHTTP` transport, provide a `DialContext` whose `net.Dialer.Control` inspects the actual dialed `net.IP` and rejects loopback/private/link-local addresses before the TCP handshake completes.

- [ ] **Step 4: Run test to verify it passes**
  Run: `rtk go test -v ./internal/web/handlers/content`
  Expected: PASS.

---

### Task 11: RCON Client Pool Socket Eviction and Close (Finding 11)

**Files:**
- Modify: `internal/infra/rcon/rcon.go:210-237`
- Test: `internal/infra/rcon/rcon_test.go`

**Interfaces:**
- Produces: `(p *Pool) Evict(addr string) error`, `(p *Pool) Close() error`

- [ ] **Step 1: Write unit test for `Pool.Evict` and `Pool.Close`**
  Verify that eviction closes the client socket and removes it from `p.clients`.

- [ ] **Step 2: Run test to verify failure**
  Run: `rtk go test -v ./internal/infra/rcon -run TestPool_EvictAndClose`
  Expected: FAIL (`Evict` not defined).

- [ ] **Step 3: Implement `Evict` and `Close` on `Pool`**
  1. `Evict(addr string)` locks pool mutex, removes `addr`, and calls `client.Close()`.
  2. `Close()` closes and removes all active clients in the pool.

- [ ] **Step 4: Run test to verify it passes**
  Run: `rtk go test -v ./internal/infra/rcon`
  Expected: PASS.

---

### Task 12: Cap SQLite Open Connection Pool (Finding 12)

**Files:**
- Modify: `internal/infra/store/store.go:21-28`
- Test: `internal/infra/store/store_test.go`

**Interfaces:**
- Consumes: `db.SetMaxOpenConns(1)`

- [ ] **Step 1: Write test asserting `store.DB().Stats().MaxOpenConnections == 1`**

- [ ] **Step 2: Run test to verify failure**
  Run: `rtk go test -v ./internal/infra/store -run TestStore_MaxOpenConns`
  Expected: FAIL.

- [ ] **Step 3: Add `db.SetMaxOpenConns(1)` in `store.Open`**

- [ ] **Step 4: Run test to verify it passes**
  Run: `rtk go test -v ./internal/infra/store -run TestStore_MaxOpenConns`
  Expected: PASS.

---

### Task 13: Final Architecture and Full Regression Verification

**Files:**
- Test: `internal/arch/arch_test.go`
- Test: Entire test suite (`task test`)

- [ ] **Step 1: Run DDD architecture ratchet tests**
  Run: `rtk go test -v ./internal/arch`
  Expected: PASS (zero layer breaches).

- [ ] **Step 2: Run full project test suite with race detector**
  Run: `rtk task test`
  Expected: 100% PASS across all packages.
