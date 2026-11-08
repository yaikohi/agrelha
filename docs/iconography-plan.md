# Spec: iconography — adopt Pixelarticons (AGR-76)

Status: **specified, not implemented.** Written 2026-11-08. Awaiting review before Plan.

## Assumptions I'm making

1. The set is settled by the ticket: Pixelarticons. This spec picks the *glyphs*, the
   *sizes* and the *mechanism*, not the family.
2. "Sensibly picked, readable and pretty" is the acceptance bar, so every choice below
   carries its evidence: one icon family, one grid, one scale, and a named reason for
   each glyph over its nearest alternative.
3. Icons are additive. No existing label, lamp or tag is replaced by one, and nothing
   about `components.Lamp` changes — it stays the one status indicator.
4. Penpot stays the design-system spec source for tokens, but it holds no icon library
   today, so this lands code-first. Penpot parity is a follow-up, not a blocker.
5. First adoption is deliberately narrow — three sites, not every affordance in the app.

→ Correct me now or the Plan phase proceeds on these.

## Objective

Give Hearth an icon vocabulary that reads as an operator console's, not a game HUD's:
monochrome, on the same hard grid as Departure Mono and `radius-control: 2px`,
self-hosted, and crisp on every display.

Success: a reader can scan the mods panel, the backups panel and an instance's
controls by shape as well as by word, and no icon is softer, busier or off-grid than
the type it sits beside.

## What exists today

| Thing | Where | Reality |
|---|---|---|
| `icon(url string)` | `web/pages/mod_detail.templ:54` | A remote mod thumbnail. Content, not a system. Unrelated to this work and left alone |
| Icon vocabulary | — | None. `DESIGN.md` and `docs/design-system.md` do not contain the word |
| Colour guard | `web/pages/design_tokens_test.go` | Globs every `.templ` in `pages` and `components`, fails the build on a raw hex or a Tailwind palette class |
| Asset pipeline | `web/efs.go` (`//go:embed all:assets`), Taskfile `assets:fonts` / `assets:datastar` | Pinned version, `curl` from jsdelivr, `status:` guard so it runs once, file committed and embedded in the binary. Icons follow this exactly |
| Component rules | `web/components/README.md` | One visual atom per file; may import stdlib, templ and `internal/domain` only |

## Decisions, with the evidence

**The default family, never the sharp one.** Pixelarticons 2.4.2 ships 1036 SVGs, of
which 332 are `-sharp` variants and 53 are `-solid`. A set that covers only a third of
the names cannot be the house family, and mixing the two inside one screen is the exact
thing that makes an icon set look assembled rather than drawn. Default everywhere;
`-sharp` and `-solid` are not used, even when they are the better single glyph.

**MIT, and the notice ships with the files.** `LICENSE` in the upstream repo is the MIT
licence, "Copyright (c) 2019 Gerrit Halfmann". MIT requires the notice to travel with
substantial portions, and the SVGs are embedded in the binary, so the licence text is
vendored to `internal/web/assets/icons/LICENSE` and is embedded with them.

**`currentColor` needs no post-processing.** All 43 candidate files carry
`fill="currentColor"` on the `<svg>` element, so they inherit `text-ink-muted` and the
hex guard has nothing to object to. Not assumed for the rest of the set: a test asserts
it per vendored file.

**Integer scales only: 24px and 48px.** This is the one place this spec contradicts the
ticket, using the ticket's own rule. The glyphs are drawn on a 24-unit grid, so one
grid unit is one CSS pixel at 24px and two at 48px. At the ticket's 16px a unit becomes
0.667px, which antialiases on every display; at 12px it is 0.5px, crisp on a 2× screen
and grey mush on a 1× one. "A pixel icon at a non-integer scale stops being a pixel
icon" rules both out. `h-6 w-6` and `h-12 w-12` are existing steps, so no new token.

What that costs, stated plainly: **`text-xs` rows get no icons.** That is the tab bar
(`components/tab_bar.templ`, `font-pixel text-xs`), the telemetry line, the installed-mod
rows and the result metadata. A 24px glyph beside 12px type is a badge, not an
annotation. The remedy, if those rows ever need icons, is a hand-drawn 16-grid subset —
the ticket's own argument for a pixel set — and that is out of scope here.

**Every coordinate must be an integer.** Measured across the candidates: all are
integer-only except `arrow-up-box`, which carries `-8.933` and `4.009` and is therefore
off-grid and excluded. A test enforces this for every vendored file, because an off-grid
glyph is invisible in review and obvious on screen.

**Inline SVG, not a sprite and not `<img>`.** `<img src="…svg">` cannot inherit
`currentColor`, which the colour rule requires. An external `<use href="sprite.svg#id">`
inherits unreliably across browsers. Inline markup from the vendored files costs no
request, is already in the binary, and is the only option where the colour rule holds.

## The vetted set

Readability proxy: **cmds** is the number of path commands, i.e. how much detail is
packed into 24×24. Under ~60 reads cleanly; over ~100 is a risk even at 24px and is
flagged. Where two glyphs fit a concept, the simpler one wins unless the busier one is
the conventional shape.

| Concept | Glyph | cmds | Why this one |
|---|---|---|---|
| World / instance | `globe` | 80 | A world is the domain's own word. `gamepad` (45) reads as a game HUD, which is the risk the ticket names |
| Mods | `package` | 90 | The conventional package shape. `box` (85) is no simpler and says less |
| Configs | `sliders` | 50 | Half the detail of `settings-cog` (110) and `gear` (116), and it says "values you set" rather than "preferences" |
| Console | `terminal` | 37 | Exact, and among the simplest in the set |
| Logs | `notes` | 65 | Lines on a page. `script` (36) is simpler but reads as a scroll |
| Backups | `archive` | 42 | Simpler than `save` (52) and `database` (92), and backups are archives, not disks |
| Admins | `users` | 77 | — |
| Grant / revoke | `user-plus` / `user-x` | 58 / 62 | A matched pair, which is what makes the two read as opposites |
| Access requests | `inbox` | 37 | Things waiting for the operator. `mail` (47) says correspondence |
| Start | `play` | 42 | — |
| Stop | `power` | 52 | Stopping a world is shutting a server down, not pausing media. `stop-solid` (14) is a filled square, and it is `-solid` |
| Restart | `reload` | 58 | Simpler than `refresh` (78); `repeat` (65) means loop |
| Install | `plus-box` | — | Boxed, to pair with removal |
| Remove | `minus-box` | 27 | `trash` (36) reads as destruction. Removing a mod takes effect next restart and is reversible, and `VariantDanger` is reserved for the genuinely destructive |
| Delete (world, backup) | `trash` | 36 | The one place a bin is honest |
| Export / import | `download` / `upload` | 48 / 38 | A matched pair |
| Search | `search` | 68 | — |
| Filter / sort | `filter` / `sort-vertical` | 50 / 52 | Pairs with the new search controls |
| Installed / done | `check` | 50 | `checkbox-on` (47) implies a control you can toggle |
| Open elsewhere | `external-link` | 71 | For the Thunderstore and Modrinth links |
| Warning / deprecated | `warning-diamond` | 112 | **Risk.** The only warning glyph in the set and the busiest thing here. Verify at 24px before adopting; if it mushes, these states stay text-only |
| Incident | `bug` | 98 | Busy. `zap` (66) is the fallback if `bug` does not hold up |

Excluded on evidence: `arrow-up-box` (off-grid), every `-sharp` and `-solid` variant
(incomplete families), `loader` (the pulsing dot already carries in-flight state).

## Where they land first

1. **Section heads** — `components.SectionHead`, 24px before the `h2`. Mods, Configs,
   Console, Backups, Admins, Access.
2. **Empty states** — 48px above the sentence. "No mods installed", "No backups yet",
   the search empty state. Pure gain: those panels are currently a line of grey text.
3. **Instance controls** — Start, Stop, Restart, Back up. `ButtonClass` already sets
   `inline-flex items-center gap-2`, so a 24px glyph needs no new layout. Only these
   four; a button with an icon is a decision, not a default.

Not in the first pass, and not without a 16-grid subset: tab bar, telemetry, mod rows,
installed list, toasts.

## Tech stack

Go 1.25 · templ · Tailwind (hearth tokens) · Pixelarticons 2.4.2 (MIT), vendored. No
new Go or JS dependency.

## Commands

```
Vendor icons: task assets:icons     # new: pinned curl + status guard, like assets:fonts
Generate:     task generate
Build:        task build
Test:         task test
Vet:          task vet
Dev:          task dev
```

## Project structure

```
internal/web/assets/icons/*.svg        → vendored glyphs, embedded via web/efs.go
internal/web/assets/icons/LICENSE      → upstream MIT notice, travels with the files
internal/web/components/icon.templ     → Icon, the only renderer
internal/web/components/icon_test.go   → grid, colour and name coverage guards
docs/design-system.md                  → new Iconography section: the set, the two
                                         sizes, the "never the only signal" rule
```

## Code style

`Icon` takes a name and a size, nothing else, and the markup comes from the vendored
file so the component holds no path data:

```go
type IconSize string

const (
	IconInline IconSize = "h-6 w-6" // 24px, one grid unit per CSS pixel
	IconHero   IconSize = "h-12 w-12"
)

templ Icon(name string, size IconSize) {
	<span class={ "inline-block shrink-0 " + string(size) } aria-hidden="true">
		@templ.Raw(iconSVG(name))
	</span>
}
```

`iconSVG` reads the vendored file from the embedded FS once at startup and panics on an
unknown name, so a typo fails the first render rather than rendering nothing. Icons are
`aria-hidden` because the rule is that they never carry a signal alone: the label beside
them is the accessible name.

Conventions: role classes only, never a colour; no new code comments; one component per
file, as `components/README.md` requires.

## Testing strategy

`go test` beside the code, as everywhere else in this repo.

| Level | What |
|---|---|
| components | Every vendored file carries `fill="currentColor"` and `viewBox="0 0 24 24"` |
| components | Every coordinate in every vendored file is an integer — the `arrow-up-box` class of defect |
| components | Every name `Icon` is called with across `pages` and `components` exists in the vendored set, and every vendored file is used by something. A set that grows faster than its use is how icon sets rot |
| components | `Icon` renders only the two sizes; an unknown size does not compile |
| pages | The existing `design_tokens_test.go` already fails on a hex, and now covers the new markup for free |
| manual | One screenshot pass at 24px on a 1× display before the set is called done. `warning-diamond` and `bug` are the two to look at |

## Boundaries

- **Always:** pin the upstream version in the Taskfile; keep `LICENSE` beside the SVGs;
  `task generate` after touching `.templ`; `task test` and `task vet` before done.
- **Ask first:** adding a glyph outside the vetted table; drawing one by hand; putting an
  icon in a `text-xs` row; touching `components.Lamp` or `mod_detail.templ`'s `icon()`.
- **Never:** a CDN reference at runtime; a raw hex or a Tailwind palette class; an icon as
  the only signal for a state; a `-sharp` or `-solid` variant; a non-integer size.

## Success criteria

1. `internal/web/assets/icons/` holds only vetted glyphs plus `LICENSE`, every file from
   Pixelarticons 2.4.2, every one `viewBox="0 0 24 24"`, `fill="currentColor"`, integer
   coordinates.
2. The app renders icons with no network access — verified by running with the network
   down, since that is the LAN-behind-WireGuard case the ticket requires.
3. `components.Icon` is the only place SVG markup is rendered; no `<svg>` appears in any
   page template.
4. Icons appear at exactly two sizes, 24px and 48px.
5. Every icon sits beside a text label or an already-labelled control. Removing every
   icon from the page loses no information.
6. The three adoption sites are done: section heads, empty states, the four instance
   controls.
7. `docs/design-system.md` has an Iconography section, and `docs/adr/` records the family,
   the licence and the two-size rule.
8. `task test` and `task vet` pass.

## Progress

**Implemented 2026-11-08.** `task test` and `go vet ./internal/...` pass.

| Added | Where |
|---|---|
| `assets:icons` — pinned 2.4.2, `status:` guard, licence | `Taskfile.yml`, `internal/web/components/icons/` |
| `Icon`, `Empty`, `IconStandard`, `IconHero` | `internal/web/components/icon.templ`, `icon.go` |
| Grid, colour, licence and set-in-use guards | `internal/web/components/icon_test.go` |
| Glyph on a section head | `components/card.templ` (`SectionHead` takes an icon) |
| Glyph on a lifecycle control | `pages/helpers.go` (`ActionUI.Icon`), `pages/instance.go`, `pages/instance.templ` |
| Empty states | `pages/mods.templ`, `pages/manager.templ`, `pages/valheim_instance.templ` |
| Iconography section, ADR 0010 | `docs/design-system.md`, `docs/adr/0010-pixel-icons-on-an-integer-grid.md` |

Three deviations from the spec above, all deliberate:

1. **The glyphs live in `internal/web/components/icons/`, not `assets/icons/`.**
   `go:embed` cannot reach a parent directory, so keeping them under `assets/` would
   have made a leaf component import the web root for its embedded FS. They are
   component source data read at render time, not assets served to a browser.
2. **Seven glyphs vendored, not twenty-two:** `archive`, `globe`, `package`, `play`,
   `power`, `reload`, `sliders` — exactly what the first adoption renders. The test
   fails a vendored glyph nothing uses, so the set cannot run ahead of the product.
   The rest of the table stays the decided vocabulary, vendored when first used.
3. **`warning-diamond` and `bug` were not vendored.** Both are busy enough to need a
   look on screen, and the first adoption did not need them. Open question 2 stands.

## Open questions

1. The 16px row. This spec drops icons from the tab bar and every `text-xs` line rather
   than render them at a fractional scale. The alternatives are to accept soft edges at
   16px, or to hand-draw a 16-grid subset of about eight glyphs later. Which?
2. `warning-diamond` at 112 commands is the busiest glyph adopted and the only warning
   shape available. Adopt it provisionally and judge it on screen, or leave warnings
   text-only until a simpler one is drawn?
3. Penpot holds no icon library. Add the adopted set to it as components after the code
   lands, or leave icons out of the Penpot spec and say so in `DESIGN.md`?
