# Accounts and Grants: Zitadel Decides, agrelha Enforces

agrelha is no longer a single-operator tool. Several people can sign in, and
each sees and operates only the Instances they hold a Role for. Zitadel is the
authority on who holds what; agrelha reads that from the token and enforces it.

## Context

Until now authorization was one line in the OIDC callback: a comma-separated
`ALLOWED_EMAIL` env var, checked against the token's email claim. Everyone who
passed it was equally omnipotent, and adding a person meant editing a ConfigMap
and restarting the pod.

The need that broke this: friends who join over WireGuard should be able to run
their own worlds — start, stop, restart, read logs, edit mod configs, install
mods, take backups — and stand up a world of their own — without being able to
touch anyone else's world, delete worlds, or reach the admin pages.

Authentication was never the gap. `infra/auth/oidc` already implemented the full
Authorization Code flow against Zitadel, with signed session cookies and
RP-initiated logout. Only authorization was missing.

## Decision

**Zitadel is authoritative for per-Instance permission.** Roles arrive in the ID
token under `urn:zitadel:iam:org:project:roles` — the same claim Grafana already
reads in this cluster. agrelha keeps no grants table. A second store that
governs nothing is a liability, and two stores that both claim authority is
worse.

Three kinds of Role: `agrelha-admin` (everything), `agrelha-user` (sign-in
only), and `agrelha-<game>-<NN>` (operate one Instance). Holding *any* of them
permits sign-in; which ones you hold decides what you can do.

**Per-Instance Roles are keyed on `(GameID, Number)`, not on Slug.** Number is
the real primary key and never changes; a Slug follows the world's name and
does change. A renamed world must not silently drop everyone's access.

**agrelha mints and retires those Roles itself**, as a Zitadel service user
scoped to the agrelha project alone. It speaks the stable Connect-RPC resource
APIs — `zitadel.project.v2.ProjectService` for Roles and
`zitadel.authorization.v2.AuthorizationService` for Grants — not the deprecated
`management/v1` REST surface. Both were verified present on the deployed
instance (Zitadel v4.15.1), and the request shapes are pinned by an httptest
contract test rather than inferred from documentation.

Creating a world mints its Role before any manifest is written or any commit is
made; if Zitadel refuses, the creation fails with nothing to undo. Deleting a
world removes the Role, which cascades to every grant Zitadel holds against it.

**Sessions moved server-side**, into SQLite, with a 2 hour TTL renewed at the
half-life. The cookie now carries a session id rather than the identity itself.

**Creating a world is allowed; creating many is rationed.** An Account may create
one world unaided and is Granted its role automatically. Beyond that it files a
Request the agrelha operator approves or denies, because creation spends a RAM
and slot budget everyone shares and nothing else bounds it. Approval performs the
creation and the Grant together. Deletion stays with the operator: it is
irreversible and frees a slot someone else is waiting for.

The cap counts worlds an Account *created*, not worlds it can reach, so a world
the operator Grants someone does not consume their allowance. That required
Instances to record a creator, which they did not before.

## Consequences

**Revocation is not instant by default, and that is a deliberate trade.** A Role
removed in Zitadel still sits in the holder's existing session until it expires
— at most two hours. The `/accounts` page therefore has a "Sign out everywhere"
button, which deletes every session row for an Account and forces a fresh token
on the next request. Revoke the Grant, then press it.

**You cannot grant access to someone who has never signed in.** The service-user
credential is scoped to the project, so agrelha cannot read Zitadel's user
directory. It learns an Account's `sub` only from a sign-in. This is a real
constraint of choosing the smaller blast radius, and the `/accounts` page says
so where the list would otherwise look broken.

**Instance Numbers are reused, so a stale Role is a security bug.** Delete
Valheim 02, create a new world, and it is `agrelha-valheim-02` again — any
surviving grant would now authorise a different world. Hence the Role is treated
as a slot reservation: minting it is the first external write of `CreateInstance`,
and `ErrRoleExists` is fatal. A collision means a previous retirement failed and
a human should look. The structural fix is an immutable instance id independent
of Number; that is not done.

**Enforcement is route middleware, not the use case.** `InstanceManager` was the
tempting place — identity already reaches it as the audit actor — but it cannot
see reads (`ListBackups`, `InstanceLogs` and several others take no actor and
sometimes no context), its `actor ...string` is optional-by-signature and
defaults to `"-"`, and a denial there would surface through the toast helper as
"Start failed: forbidden" with status 200. So `web/guard` resolves a Principal
once per request and refuses in whichever of three shapes the caller asked for:
a redirect for a page, a Datastar patch for a stream, JSON for an API call.

The Datastar branch returns **200**, not 403, because the client treats a
non-2xx as a transport error and discards the patch — a correct status code
would render nothing at all.

**Route middleware fails open when a route matches no mount, and the route
namespace has three per-Instance URL shapes** (`/api/{game}/:num`,
`/api/{game}/instances/:num`, `/{game}/provisioning/:num`). Mounting only the
obvious one would leave start, stop and restart wide open while looking correct.
`web/guard`'s coverage test walks the real route table and fails the build on any
protected route that is not explicitly classified, the same way the architecture
ratchet fails on an unclassified package.

**Several fail-opens were load-bearing in tests.** `IsAdmin(nil, c)` returned
true, a nil `*Authenticator` reported itself authenticated, the whole protected
group was skipped when `Auth` was nil, `nav`'s admin flag defaulted to true, and
OIDC discovery failure silently downgraded the deployment to passwordless
click-to-auth. All are gone; `AUTH_MODE=dev` is now the only way to get the dev
authenticator, and a discovery failure stops the process. Test fixtures that had
been quietly relying on those defaults — a dashboard test asserting admin-only
content as a guest, among others — were asserting behaviour that only ever
happened in tests.

## Alternatives rejected

**Per-Instance grants in agrelha's own database.** Immediate revocation, no IdP
credential, no orphan Roles, and no Zitadel work when a world is created. It
loses the single source of truth, and splits "who may do what" across two
systems that can disagree.

**Scopes instead of Roles.** Scopes are requested by the client and are identical
for every person using it; they carry no per-person information. OIDC has no
notion of a scope granted to one user and not another. Roles, via user grants,
are the mechanism for that.

**Terraform-managed Roles.** The official `zitadel/zitadel` provider has
`zitadel_project_role` and `zitadel_user_grant`, and would fit the existing IaC
pattern. It cannot keep up with worlds created from the wizard without a
`tofu apply` per world.
