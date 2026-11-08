# Plan: the front page, widened

Status: **design settled 2026-11-08, not built.** Mockup revision 3 is the reference.
Scope is the signed-out/non-admin front page only — `pages.Panel`. The instance and
mod-search layouts are agreed on paper and deferred.

## What is being built

A wide front page for people who are not operators: a masthead that says the state in
words, a sticky rail of games, the burning seam between rail and content, and the
worlds as cards that go two across at 1180px and three at 1660px.

Today the same page is a 1024px column of full-width rows, one world per screenful.

## What has to change, in dependency order

### 1. A wide shell, without widening anything else

`LayoutHearth` is used by the panel **and** by the manager and instance pages, so
widening it widens three redesigns that have not happened. Add a sibling instead:

```
layout.templ:20  LayoutHearth(title)       → stays max-w-5xl
layout.templ:+   LayoutHearthWide(title)   → max-w-[1680px]
panel.templ:8    Panel uses the wide one
```

The footer in `layoutShell` shares the cap, so it takes the same treatment. Nothing
else moves until its own page is redesigned.

**Check:** manager, instance and admin-hub pages render byte-identically.

### 2. `BuildPanel` groups by game and counts

`PanelUI` is flat today — `Ports` with a `Game` label on each, plus `Idle` for games
with no worlds. The new page needs the games as sections and a rail entry each:

```go
type PanelGameUI struct {
	Slug   string   // valheim — the anchor and the fire's tint key
	Label  string
	Tint   string
	Meta   []string // "Two worlds, one lit", "0.220.5 · BepInEx"
	Worlds []PanelPortUI
	Hint   string   // only when Worlds is empty
}

type PanelTallyUI struct{ Lit, Playing, Worlds int }
```

`PanelUI` becomes `{Games []PanelGameUI, Tally PanelTallyUI}`. Every input already
exists: `ServerRowUI.Online`, `PlayersKnown`, `Players` give the tally, and the
grouping is the loop `BuildPanel` already runs over `games`.

**Check:** table test over the four shapes — no games, a game with no worlds, worlds
with unknown player counts, everything lit.

### 3. The headline is generated copy

"Two worlds are lit. One is sleeping." is the tally in words, so it needs the same
care as any other string in the product: singular and plural, and the second sentence
dropped when nothing is asleep.

```
3 lit, 0 asleep  → "Three worlds are lit."
1 lit, 2 asleep  → "One world is lit. Two are sleeping."
0 lit, 1 asleep  → "Nothing is running right now."
0 worlds at all  → "No worlds yet."
```

**Check:** a table test per row above. This is where a plural bug would ship.

### 4. The page itself

`panelMasthead` gains the headline and the tally; `panelPort` becomes a card
(`panelWorld`); `panelIdle` becomes the empty card inside its game's section; the rail
and the seam are new. `Start a world` renders only when the viewer is an admin.

Keep: the copy button and its signals, the lamp classes, the address as the loudest
thing on the card, `max-w-prose` on every sentence.

**Check:** `pages/design_tokens_test.go` already globs this file. Add a render test
that the rail lists every game and that a game with no worlds still appears.

### 5. The seam fire

The shader from the Seam artifact, self-hosted as `assets/js/seam-fire.js` and
embedded like the other assets. It reads `--moss`, `--sky`, `--flame` and `--surface`
off the computed style, so it follows the tokens and both themes without a second
palette. Per-world lamp fireballs are **out** — they read as knots in the flame.

- Idles when scrolled out of view (`IntersectionObserver`), stands down entirely under
  `prefers-reduced-motion`, and the column is `display:none` below 1000px.
- No WebGL, no canvas: the seam renders as a 1px rule. The page must be whole without it.
- The fire ramp is five baked hex colours in JS — the one place in the system that is
  not a token. It is a fire, not a surface, and `design-system.md` should say so.

**Check:** the page with JS disabled, the page at 900px, and the page with reduced
motion on. None of the three may lose information.

## Slices

1. `LayoutHearthWide` + the panel switched to it. Nothing else changes; the page is
   just wider. **Ship-able alone.**
2. `BuildPanel` restructure + tests, page still rendering the old rows.
3. The masthead, headline and tally.
4. The rail, the sections and the world cards.
5. The seam fire.
6. Presence chips — the two accessors above, through `ServerRowUI`, into the card.

Each slice ends with `task test`, `task vet`, and a look at the running app.

## Risks

| Risk | Mitigation |
|---|---|
| The wide shell makes the *other* pages look unfinished by comparison | They keep their 1024px cap; that is the whole reason for the sibling layout |
| The fire costs a GPU on a page friends open on phones | Hidden under 1000px, idle off-screen, off under reduced motion |
| A game's version line has no source | See open questions |
| Named presence does not exist | The chips fall back to the count line that ships today |

## Open questions

1. ~~**The rail's second line.**~~ **Resolved 2026-11-08.** It is the world's flavour,
   not a build number — the mockup's "0.220.5" was invented and no Valheim version
   exists anywhere in the codebase. Per game:

   | Game | Rail line | Source today |
   |---|---|---|
   | Valheim | `BepInEx` or `Vanilla` — a world is one or the other | `ServerRowUI.VersionBadge` (`helpers.go:571`), already exactly this |
   | Minecraft | `1.21.1 · neoforge` — the Minecraft version and the loader **type**, never the loader's own version — or `Vanilla` | `VersionBadge` (`helpers.go:640`), already exactly this: `Loader` is the enum name |
   | Garry's Mod | a workshop collection, always. Shape to be designed when it is built | — |
   | Anything later | designed when it arrives | — |

   **Sub-decision made with it:** when the worlds of one game disagree — one vanilla,
   one modded, or two Minecraft worlds on different versions — the rail line is
   **omitted**. One line cannot say two things, and each card already carries its own
   badge. The rail only speaks when every world in the game agrees.
2. ~~**Named presence.**~~ **Resolved 2026-11-08: build the chips.** Both names already
   exist and are discarded at the last step, so this is plumbing, not new machinery:

   | Game | Where the names are | What is thrown away today |
   |---|---|---|
   | Minecraft | `access.AccessManager.OnlinePlayers` — RCON `/list`, parsed by `ParsePlayerList` | `wiring.go:627` keeps only `len(players)` |
   | Valheim | the `players` table's `character` column, beside `steam_id` and `online` (`store.go:48`); per world through `instance_presence` | `store.CountOnline` counts rows |

   Two rules that come with it: a Valheim player whose character name was never seen
   folds into a trailing **"+N more"** chip rather than showing a Steam ID, and the
   chips are **server-rendered** — the live Datastar signal keeps the *count* current,
   so the names can lag it until the next render. If that gap shows, the fix is to
   patch the chips over the same SSE stream, not to poll.
3. ~~**`Join` as the one filled button.**~~ **Resolved: outline everywhere.** No card
   outranks another; the address button carries the page and the one filled accent
   stays unspent.
4. ~~**The admin hub.**~~ **Resolved: later.** `/` still renders `AdminHub` for an
   admin, so this redesign is invisible to a signed-in operator until that page has its
   own pass. The rail and the world card are therefore built as components `AdminHub`
   can adopt, not as markup inlined into `Panel`.
