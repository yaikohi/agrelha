# Personas

Three people use agrelha. This document describes who they are, what they are
trying to get done, and what the product currently does to them on the way.

## How to read this

**These are archetypes, not research.** Nobody was interviewed. They are drawn
from the product's own shape — the routes, the guard, the Rules in
[CONTEXT.md](../CONTEXT.md) — and from things that demonstrably happened while
building it.

**Friction is evidenced or it is labelled.** Every friction point below is
either `[verified: <where>]`, meaning you can go and look at it, or
`[unverified]`, meaning it is a guess about how someone feels. The two are never
blurred. A persona document that mixes "the Delete button is hidden while a
world is crash-looping" with "users probably feel anxious" is worth nothing,
because you cannot tell which half to act on.

**This describes today, not tomorrow.** There is no "should" in this document.
Feature ideas are a separate pass, so that the mapping stays honest instead of
bending to justify things we already want to build.

**Personas are not Roles.** `domain.Principal` and the `guard` package already
answer "may this request do this thing", correctly and under test. These
personas answer "who is this and what are they trying to do", which is a
different question with a different use.

## The three, and what they are called

| Persona | In the ubiquitous language | Signs in? |
|---|---|---|
| **Player** | [`Player`](../CONTEXT.md) — someone who joins a game server | No |
| **Keeper** | An `Account` holding one or more `Grant`s | Yes |
| **the agrelha operator** | The `Account` holding `agrelha-admin` | Yes |

Two others appear in these journeys without being personas:

- **Operator** (in-game) — a Player with in-game powers, who may have no agrelha
  Account at all. They never see the product; they create work for a Keeper.
- **Newcomer** — not a person but Keeper's first stage, on the day they sign in
  with nothing.

**the agrelha operator inherits everything Keeper has.** Creating an Instance
Grants its creator access to it, so the operator is a Keeper too. Their card
below lists only what is *exclusively* theirs.

---

# Player

Someone who wants to get into a game with their friends. They did not come here
to use agrelha; they came here because someone sent them a link.

**Job to be done.** *Get into my friend's world, now, without reading anything.*
The job recurs every session and the bar is a minute or two. agrelha is a
waypoint between a chat message and a game client — never a destination.

### Pains

1. **They cannot tell whether they are allowed in.** The public panel shows an
   address, a password and a Join button, and says nothing about Admission. On a
   Minecraft world the whitelist decides, so a Player who is not on it is
   refused — and only finds out in the game client, after leaving agrelha.
   `[verified: pages/panel.templ has no whitelist or admission text;
   CONTEXT.md "Admission"]`
2. **Being refused is a dead end.** Nothing on the page tells them who to ask or
   offers to ask for them. The next step is out-of-band and they have to invent
   it. `[verified: the only public routes are /, /sse, /login, assets and mod
   export — guard/coverage_test.go:23]`
3. **"Is it worth launching the game?"** is answered only by a lamp and a player
   count, which they must already know how to read. `[unverified: nobody has
   said this is confusing]`

### Gains

1. The address and password are **on the page, in clear, with no login** — for
   the password-world case this is close to frictionless, and deliberately so.
   `[verified: guard/coverage_test.go:24; pages/panel.templ:63-68]`
2. **Live state without refreshing** — the lamp and player count stream over SSE,
   so the page answers "is anyone on" at a glance. `[verified: /sse is public]`
3. **The mod list is downloadable without an account**, which is what a modded
   world actually requires before joining. `[verified: /mods/export and
   /api/<game>/:num/mods/export are public]`

### One unexpected insight

**The most valuable thing on the public page may be the password — and it is
also the one thing a security review would flag first.** It is readable by
anyone who loads `/`. For Valheim that *is* the Admission mechanism, so the page
is handing out the key to the server.

This is worth stating plainly because the instinct is to "fix" it, and fixing it
would convert the product's smoothest journey into its worst. The question is
not whether to hide it but whether the people who can reach the page are already
the people who should be in the world — which, behind the WireGuard VPN, they
largely are. `[verified: the exposure; [unverified]: that this is a problem at
all]`

### Product fit

Strong for a password world. On a whitelist world agrelha gets them to the
threshold and abandons them there, which is the single widest gap in the
product's coverage of this persona.

---

# Keeper

Someone who has a world of their own and looks after it. They sign in, they
install mods, they fix things when they break. They are not an administrator of
agrelha and cannot become one.

**Job to be done.** *Keep my world running and current, without having to
understand how it is hosted.* The job is bursty: nothing for a week, then an
hour when a mod update breaks the server or a friend cannot get in.

### Pains

1. **They cannot get a friend into their own world.** The capability exists and
   is almost unreachable. For Valheim and GMod a Keeper is authorised for
   `/<game>/access` — but the only link to any access page in the entire UI sits
   behind `IsAdmin`, so they must know and type the URL. For Minecraft, the one
   game where Admission actually blocks people, the page is admin-only and they
   cannot reach it at all. `[verified: guard/coverage_test.go lists
   /valheim/access and /gmod/access under anyAccount; guard/guard.go:190 mounts
   /minecraft/access with Admin: true; components/nav.templ:23,67 is the only
   link and is IsAdmin-gated]`
2. **They cannot delete their own world.** Deletion is reserved to the agrelha
   operator, including for a world the Keeper created. When a world is unusable,
   their only move is to ask someone else. `[verified: CONTEXT.md rule "A Grant never confers deleting an Instance"]`
3. **A request for a second world disappears.** Any Account may create one
   Instance; the next files a Request. `requests.Service.ForSubject` exists to
   list an Account's own Requests and **is never called from web or wiring**, and
   `/requests` is admin-only — so after submitting, a Keeper has no page that
   shows whether it is pending, approved or denied. `[verified: CONTEXT.md rule "Any Account may create one Instance";
   guard/guard.go:189; ForSubject has no caller outside app/requests]`

### Gains

1. **A world of their own on day one.** No approval, no ticket: sign in, run the
   wizard, and the Instance is theirs, Granted automatically.
   `[verified: CONTEXT.md rule "Any Account may create one Instance"; the wizards are in anyAccountPrefixes]`
2. **Mods and configuration without a shell.** Search, install, edit a mod's
   settings, restore a backup — all without touching the host.
3. **They can see why it broke.** Console, crashes and incidents are on the
   instance itself, so a crash is something they can read rather than report.

### One unexpected insight

**A Keeper's worst moments are the ones where agrelha is confidently wrong, not
the ones where it is missing a feature.** Two shipped examples, both found and
fixed in a single session:

- A world in a Docker restart loop reported **Running**, because the adapter read
  `State.Running` — which Docker holds `true` for the whole of a crash loop — and
  never read `State.Restarting`. The lamp said healthy, and Delete is gated on
  "not running", so the one button that could clear it was hidden by the lie.
  `[verified: fixed in infra/runtime/docker/runtime.go and
  app/instances.stateFromStatus]`
- A world created from a Pack recorded Minecraft `1.21.1` — the wizard's default
  — while the server actually ran `26.1.2`. Mod search then filtered for the
  wrong version, so the compatibility advice was wrong with no sign of it.
  `[verified: fixed via domain.PackFacts and infra/installed]`

Neither was a gap in the feature set. Both were the product answering a question
it should have declined to answer. For this persona, *trust* is the feature, and
a wrong lamp costs more than a missing page.

### Product fit

Good once they have a world and the world is working. Weak at both edges: the
empty-handed first day is barely designed, and every genuine dead end — delete,
whitelist, a second world — routes to a human who is not on the page.

---

# the agrelha operator

You. The person who owns the hardware, approves what runs on it, and is the
escalation path for both personas above.

Listed here is only what is **exclusively** yours. Everything a Keeper does, you
also do, on your own worlds.

**Job to be done.** *Let people run what they want without letting the box fall
over, and without becoming a full-time helpdesk.* The job is reactive: it starts
when someone else is blocked.

### Pains

1. **You cannot grant access to someone who has not signed in.** agrelha's
   identity credential is scoped to its own project and cannot read the user
   directory, so it learns an Account's `sub` only from a sign-in. Every grant is
   therefore a two-step dance with an out-of-band message in the middle: *sign in
   first, then tell me, then I can add you.* `[verified: CONTEXT.md rule "A Grant can only name an Account that has signed in"]`
2. **You are the only route past every dead end.** Deleting any world, approving
   any second world, whitelisting on Minecraft, granting anything — all of it is
   yours alone. Each Keeper pain above terminates at you.
   `[verified: guard/guard.go:184-192 mounts /access, /accounts, /requests,
   /history, /overview and /minecraft/access as Admin: true]`
3. **Nothing tells you a Keeper is stuck.** Requests are visible at `/requests`,
   but a Keeper blocked on a whitelist or a crash-looping world produces no
   signal at all — they have to reach you by hand. `[unverified: no notification
   surface was found, but absence of evidence is not proof here]`

### Gains

1. **Everything is in one place** — accounts, grants, requests, history,
   capacity and incidents are all pages in the same app.
2. **Authorization is not yours to maintain.** The identity provider decides and
   agrelha only enforces, so there is no second permission store to keep in sync.
   `[verified: CONTEXT.md rule "The identity provider decides who may do what"]`
3. **Deletion is safe by construction.** It cannot be delegated by a Grant, so a
   Keeper cannot destroy a world you would have to restore.
   `[verified: CONTEXT.md rule "A Grant never confers deleting an Instance"]`

### One unexpected insight

**Most of what is exclusively yours is exclusive for safety, but is experienced
by everyone else as absence.** The guard is correct — deletion really should not
be delegable, and the whitelist really is sensitive. But a Keeper does not
experience `Admin: true` as a safety property; they experience a missing page and
a message they have to write to you.

The interesting design space is therefore not "give Keepers more permissions"
but "make the handoff visible" — the product currently models the permission
boundary perfectly and the handoff across it not at all.

### Product fit

The operator is the best-served persona, which makes sense: the product was built
by one, for one, before Accounts existed. The newer personas are the ones with
unfinished edges.

---

# Journeys

Each journey runs from the event that triggers it to the goal, including the
steps that happen outside agrelha. Those outside steps are where most of the
friction turns out to live.

## Player — Join a world I was told about

This journey forks on **Admission**, not on game: one branch lets a stranger in
off the public page, the other requires a human. `[verified: CONTEXT.md "Admission"]`

### Branch A — a password world (Valheim, GMod)

| # | Step | Where | Friction |
|---|---|---|---|
| 1 | A friend posts "server's up" with a link | Discord | None |
| 2 | Open `/`, see the world, lamp lit | agrelha | None |
| 3 | Read the address and password off the page | agrelha | None — both are in clear, no login `[verified: panel.templ:63-68]` |
| 4 | If modded, download the mod list | agrelha | Public, no account `[verified: /mods/export]` |
| 5 | Paste into the game client and join | Game | Manual copy; a copy helper exists for Valheim `[verified: ValheimWorldPasswordUI.CopyScript]` |

**Goal reached.** This branch is close to frictionless and is the product at its
best.

### Branch B — a whitelist world (Minecraft)

| # | Step | Where | Friction |
|---|---|---|---|
| 1 | A friend posts "server's up" | Discord | None |
| 2 | Open `/`, see the world and its address | agrelha | **The page does not say Admission is a whitelist** `[verified: no such text in panel.templ]` |
| 3 | Launch the game and connect | Game | — |
| 4 | **Refused** | Game | They learn they are blocked *after* leaving agrelha, from a game-client error |
| 5 | Work out who to ask | Discord | **Dead end.** agrelha offered no name and no way to ask `[verified: public routes carry no request affordance]` |
| 6 | Wait for a Keeper to whitelist them | — | Hands off to *Keeper — Get a friend into my world*, which is itself blocked |

**Goal not reached inside the product.** Steps 4-6 are entirely outside it.

## Player — Is it up before I bother?

| # | Step | Where | Friction |
|---|---|---|---|
| 1 | Wonder whether anyone is on | — | — |
| 2 | Open `/` | agrelha | None |
| 3 | Read the lamp and the player count | agrelha | Lamp semantics are learned, not labelled `[unverified]` |
| 4 | Decide to launch, or not | — | — |

The shortest and probably most-repeated journey in the product. It works, and
the lamp exists precisely for it.

## Keeper — Create my first world

The Newcomer stage. The day they sign in holding nothing.

| # | Step | Where | Friction |
|---|---|---|---|
| 1 | Given a link and told to sign in | Discord | — |
| 2 | Sign in via the identity provider | agrelha | Their Account exists only now `[verified: CONTEXT.md "Account"]` |
| 3 | Land on a page with no worlds of their own | agrelha | The empty state is the whole product at this moment `[unverified: not assessed as a design]` |
| 4 | Find and run the create wizard | agrelha | Permitted to any Account `[verified: anyAccountPrefixes]` |
| 5 | World created, Granted automatically | agrelha | None `[verified: CONTEXT.md rule "Any Account may create one Instance"]` |

**Goal reached.** The strongest of the Keeper journeys — self-serve, no approval.

## Keeper — My world is broken; work out why

| # | Step | Where | Friction |
|---|---|---|---|
| 1 | A friend says "it's down" | Discord | The Keeper usually learns from a person, not the product `[unverified]` |
| 2 | Open the world | agrelha | — |
| 3 | Read the lamp | agrelha | **Historically wrong**: a crash loop read as Running `[verified: fixed; see Keeper insight]` |
| 4 | Open crashes / console / incidents | agrelha | Real and useful |
| 5 | Identify the cause | agrelha | Works when the cause is in the log |
| 6 | Fix it — or conclude the world is unsalvageable | agrelha | — |
| 7 | If unsalvageable: **stop**, because deletion is not theirs | — | **Dead end → the agrelha operator** `[verified: CONTEXT.md rule "A Grant never confers deleting an Instance"]` |

## Keeper — Hit the wall at world two

| # | Step | Where | Friction |
|---|---|---|---|
| 1 | Want a second world | — | — |
| 2 | Run the create wizard as before | agrelha | Same entry point; the wall is not announced beforehand `[unverified]` |
| 3 | A Request is filed instead of a world being created | agrelha | Decided in the handler, not the guard `[verified: anyAccountPrefixes comment]` |
| 4 | Want to check on it | agrelha | **No page.** `/requests` is admin-only and `ForSubject` is never wired `[verified]` |
| 5 | Ask the operator directly | Discord | **Dead end → the agrelha operator** |

## Keeper — Get a friend into my world

The other side of Player Branch B, and the most blocked journey in the product.

| # | Step | Where | Friction |
|---|---|---|---|
| 1 | A friend says they cannot get in | Discord | — |
| 2 | Look for where to add them | agrelha | **No link exists.** The only access link is `IsAdmin`-gated `[verified: nav.templ:23,67]` |
| 3a | Valheim / GMod: type `/<game>/access` from memory | agrelha | Authorised but undiscoverable `[verified: anyAccount]` |
| 3b | Minecraft: no route available at all | agrelha | **Refused — admin-only** `[verified: guard.go:190]` |
| 4 | Add them to Admission, or make them an Operator | agrelha | Works, once reached |
| 5 | Tell the friend to try again | Discord | — |

On Minecraft this journey cannot be completed by a Keeper at all. It becomes
*the agrelha operator — Give a friend access*.

## the agrelha operator — Approve or deny a Request

| # | Step | Where | Friction |
|---|---|---|---|
| 1 | Someone mentions they asked for a world | Discord | **No notification**; the Request waits silently `[unverified: no surface found]` |
| 2 | Open `/requests` | agrelha | Admin-only, as intended `[verified: guard.go:189]` |
| 3 | Judge it against capacity | agrelha | Budget is visible to the decision |
| 4 | Approve — the Instance is created and Granted to them | agrelha | `[verified: requests.Service.Approve]` |
| 5 | Tell them | Discord | The requester gets no in-product answer `[verified: ForSubject unwired]` |

## the agrelha operator — Give a friend access to a world

| # | Step | Where | Friction |
|---|---|---|---|
| 1 | "Can I help run your server?" | Discord | — |
| 2 | Try to grant it | agrelha | **Impossible if they have never signed in** `[verified: CONTEXT.md rule "A Grant can only name an Account that has signed in"]` |
| 3 | Tell them to sign in first | Discord | A mandatory out-of-band round trip |
| 4 | Wait for them to do it | — | No signal when they have `[unverified]` |
| 5 | Find them on `/accounts` and grant the Instance Role | agrelha | Works |
| 6 | Tell them it is done | Discord | — |

Three of six steps are outside the product, and the one that fails does so for a
sound reason — agrelha cannot read the identity provider's user directory.

---

# What the map shows

Three observations fall out of the journeys, offered as observations rather than
proposals.

**Every dead end routes to the same person.** Delete, second world, Minecraft
whitelist, any grant. The permission model is right; what is missing is that the
handoff across it is invisible from both sides.

**The product ends at its own edges.** Six of the eight journeys begin or end in
Discord. agrelha currently models none of that, which is why so much friction is
recorded here as "they have to ask someone".

**The worst failures were wrong answers, not missing ones.** The lamp that said
Running during a crash loop, and the version that said 1.21.1 for a 26.1.2
world, both cost more than any absent feature on this map — and neither would
have appeared on a feature backlog.
