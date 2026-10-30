---
version: 1
name: Hearth
description: >-
  A warm, dark-first interface built on firelight: near-black browns rather than
  neutral greys, sand-coloured text rather than white, and a single pale-yellow
  ember used sparingly for anything interactive. Nothing is rounded — a 2px
  radius on every control is effectively a square corner, and borders are
  hairlines rather than fills, so the surface reads as unlit wood with light
  falling on the few things that matter. Type is carried by one variable serif
  across the whole interface, with a pixel monospace reserved for the wordmark
  and deliberate emphasis, so the terminal register is an accent rather than the
  substrate. Density is tight (a 2px spacing grid, 11-12px
  labels) and elevation is almost absent: panels separate by seam lines and
  hairline borders, not shadow.
colors:
  canvas: "#1E1713"
  surface-raised: "#2A211B"
  seam: "#120E0B"
  edge: "#42352C"
  ink: "#EDE3D4"
  ink-muted: "#C2B1A0"
  ink-faint: "#9C8B7C"
  accent: "#F4DF75"
  accent-muted: "#EFB268"
  accent-ink: "#17110E"
  danger: "#D9594C"
  tint-sky: "#7FA6B8"
  tint-moss: "#8FA860"
  tint-flame: "#D9733A"
  lamp-lit: "#F4DF75"
  lamp-dim: "#EFB268"
  lamp-out: "#42352C"
typography:
  display-3xl:
    fontFamily: Fraunces
    fontSize: 30px
    fontWeight: 700
  display-2xl:
    fontFamily: Fraunces
    fontSize: 24px
    fontWeight: 600
  display-xl:
    fontFamily: Fraunces
    fontSize: 20px
    fontWeight: 600
  display-base:
    fontFamily: Fraunces
    fontSize: 16px
    fontWeight: 600
  prose-sm:
    fontFamily: Fraunces
    fontSize: 14px
    fontWeight: 400
  label:
    fontFamily: Fraunces
    fontSize: 12px
    fontWeight: 500
  wordmark:
    fontFamily: Departure Mono
    fontSize: 22px
    fontWeight: 400
rounded:
  control: 2px
  pill: 9999px
spacing:
  xxs: 2px
  xs: 4px
  sm: 6px
  base: 8px
  md: 10px
  lg: 12px
  xl: 14px
  xxl: 16px
  section: 20px
  gutter: 24px
  block: 32px
  wide: 40px
components:
  button-primary:
    textColor: "{colors.accent}"
    borderColor: "{colors.accent}"
    backgroundColor: transparent
    borderWidth: 1px
    rounded: "{rounded.control}"
    typography: "{typography.label}"
    padding: 6px 12px
    gap: 8px
  button-primary-hover:
    backgroundColor: "{colors.accent}"
    textColor: "{colors.accent-ink}"
    borderColor: "{colors.accent}"
  button-secondary:
    textColor: "{colors.ink-muted}"
    borderColor: "{colors.edge}"
    backgroundColor: transparent
  button-secondary-hover:
    textColor: "{colors.accent}"
    borderColor: "{colors.accent}"
  button-danger-hover:
    textColor: "{colors.danger}"
    borderColor: "{colors.danger}"
  button-link:
    textColor: "{colors.ink-muted}"
    borderColor: none
  button-disabled:
    textColor: "{colors.ink-faint}"
    borderColor: "{colors.edge}"
    opacity: 0.55
  button-loading:
    dotSize: 6px
    dotColor: inherits the variant's own textColor
    gap: 8px
  button-md:
    padding: 8px 14px
    typography: "{typography.label}"
  button-lg:
    padding: 8px 16px
    typography: "{typography.prose-sm}"
  input-field:
    backgroundColor: "{colors.seam}"
    borderColor: "{colors.edge}"
    textColor: "{colors.ink}"
    borderWidth: 1px
    rounded: "{rounded.control}"
    typography: "{typography.label}"
    padding: 8px 12px
  input-field-focus:
    borderColor: "{colors.accent}"
    outlineWidth: 2px
  input-error-text:
    textColor: "{colors.danger}"
    typography: "{typography.label}"
  toast:
    backgroundColor: "{colors.surface-raised}"
    borderWidth: 1px
    rounded: "{rounded.control}"
    typography: "{typography.label}"
    padding: 12px 16px
    gap: 12px
  toast-ok:
    textColor: "{colors.accent}"
    borderColor: "{colors.accent}"
  toast-err:
    textColor: "{colors.danger}"
    borderColor: "{colors.danger}"
  lamp:
    width: 11.2px
    height: 11.2px
    rounded: "{rounded.pill}"
  lamp-lit:
    backgroundColor: "{colors.lamp-lit}"
  lamp-dim:
    backgroundColor: "{colors.lamp-dim}"
  lamp-out:
    backgroundColor: "{colors.lamp-out}"
  lamp-unknown:
    backgroundColor: transparent
    borderColor: "{colors.edge}"
    borderWidth: 1px
  tag-quiet:
    textColor: "{colors.ink-faint}"
    typography: "{typography.label}"
  tag-accent:
    textColor: "{colors.accent}"
  tag-alarm:
    textColor: "{colors.danger}"
  nav-link-active:
    textColor: "{colors.ink}"
    borderBottomColor: "{colors.accent}"
    borderBottomWidth: 2px
  nav-link-inactive:
    textColor: "{colors.ink-muted}"
    borderBottomColor: transparent
  step-badge-current:
    backgroundColor: "{colors.accent}"
    textColor: "{colors.accent-ink}"
    borderColor: "{colors.accent}"
  step-badge-past:
    backgroundColor: "{colors.surface-raised}"
    textColor: "{colors.ink}"
    borderColor: "{colors.edge}"
  step-badge-future:
    backgroundColor: "{colors.canvas}"
    textColor: "{colors.ink-faint}"
    borderColor: "{colors.edge}"
---

# Hearth — agrelha design system

## Overview

Hearth is the design system behind **agrelha**, a self-hosted game-server manager. It is
dark-first and deliberately warm: there is no neutral grey anywhere in the palette. The darkest
value, `seam` `#120E0B`, is a brown-black; the lightest, `ink` `#EDE3D4`, is sand. The single
accent is a pale lamp-yellow used for everything interactive and for "this server is running".

The metaphor is literal and load-bearing: `seam` is the crevice between boards, `edge` is a lit
edge of wood, `surface-raised` is a panel catching more light, and the status indicator is called
a **lamp**. Naming follows the metaphor rather than abstract tiers (`surface-1`, `surface-2`), and
new tokens should too.

**Key characteristics**

- **One voice, with a pixel accent.** Fraunces (variable serif, 100–900) carries everything —
  headings, prose, labels, buttons, tags, states, logs. Departure Mono appears only on the wordmark
  and deliberate emphasis. There is no sans-serif.
- **Effectively square.** One radius token, `control: 2px`, on every control. The only exception is
  the lamp, which is a full circle.
- **Hairlines, not fills.** Separation comes from 1px borders and seam rules. Buttons are outlined,
  not filled — a filled button appears only on hover of the primary variant.
- **One accent, used sparingly.** `accent` carries interactive state and "lit". The three `tint-*`
  colours identify *which game* a row belongs to and are never used for interaction.
- **Tight density.** The spacing grid steps in 2px, and the dominant text size is 12px.
- **Near-flat.** The only shadow in the system is on the toast.

## Colors

The canonical theme is **Dark**; it is the default and the one agrelha has always shipped. Light is
documented as a delta below.

**Surfaces** — `canvas` `#1E1713` is the page. `surface-raised` `#2A211B` is a panel that catches
more light (toast, table headers). `seam` `#120E0B` is *darker* than the canvas and does double
duty: the background of inset wells (log pane, input fields) and the colour of divider rules.
`edge` `#42352C` is the visible border of controls.

**Text** — a three-step ramp: `ink` (13.93:1 on canvas), `ink-muted` (8.50:1), `ink-faint`
(5.39:1). All three clear AA. `ink-faint` is real text, not decoration — timestamps, badges and
subtitles use it — so it must never drop below 4.5:1.

**Accent** — `accent` `#F4DF75` for interactive and lit states; `accent-muted` `#EFB268` as its
lower rung; `accent-ink` `#17110E` is the *text colour on top of* an accent fill, not an accent.

**Semantic** — `danger` `#D9594C` (4.62:1). The three tints — `tint-sky`, `tint-moss`,
`tint-flame` — are per-game identity colours rendered as 12px labels, so they are text and are held
to 4.5:1.

**Lamp** — `lamp-lit`, `lamp-dim`, `lamp-out` are deliberately **decoupled from `accent`**. In dark
they happen to hold the same values `accent`/`accent-muted`/`edge` once did, but they are separate
tokens because in light they diverge entirely (see below). Never re-point the lamp at `accent`.

### Light theme (delta)

Light is an **overlay**: it redefines the 17 colour tokens and nothing else. Every non-colour token
is defined once, in `hearth-dark`, and is shared.

| token | light | contrast | dark | contrast |
|---|---|---|---|---|
| `canvas` | `#FAF6EC` | — | `#1E1713` | — |
| `surface-raised` | `#FFFFFF` | 1.08:1 | `#2A211B` | 1.19:1 |
| `seam` | `#EAE1D3` | 1.20:1 | `#120E0B` | 1.33:1 |
| `edge` | `#A48B67` | 3.01:1 | `#42352C` | **1.50:1** |
| `ink` | `#352A22` | 12.92:1 | `#EDE3D4` | 13.93:1 |
| `ink-muted` | `#53453A` | 8.53:1 | `#C2B1A0` | 8.50:1 |
| `ink-faint` | `#726253` | 5.42:1 | `#9C8B7C` | 5.39:1 |
| `accent` | `#734C09` | 7.03:1 | `#F4DF75` | 13.19:1 |
| `accent-muted` | `#9B5B11` | 4.99:1 | `#EFB268` | 9.47:1 |
| `accent-ink` | `#FDF6DF` | 7.02:1 on accent | `#17110E` | — |
| `danger` | `#CD392B` | 4.62:1 | `#D9594C` | 4.62:1 |
| `tint-sky` | `#345B6B` | 6.82:1 | `#7FA6B8` | 6.78:1 |
| `tint-moss` | `#4E5C32` | 6.71:1 | `#8FA860` | 6.69:1 |
| `tint-flame` | `#A14C22` | 5.45:1 | `#D9733A` | 5.44:1 |
| `lamp-lit` | `#6F3B9C` | 7.00:1 | `#F4DF75` | 13.19:1 |
| `lamp-dim` | `#9871BB` | 3.59:1 | `#EFB268` | 9.47:1 |
| `lamp-out` | `#CAB89C` | 1.79:1 | `#42352C` | 1.50:1 |

Light was derived to mirror dark's *hierarchy*, not to satisfy a flat floor: the ink ramp and all
three tints land within 0.04 of their dark counterparts.

**Why `accent` changes hue.** `#F4DF75` is a glow — on any pale ground it measures 1.34:1 and is
invisible. Holding its hue exactly (50.1°) and crushing lightness until it clears AA produces an
olive `#85700B`, which reads as moss rather than ember and collides with `tint-moss`. The light
accent therefore drifts 12° warm, to 38°, giving `#734C09` — still amber, still fire.

**Why the lamp changes hue entirely.** Warm-on-light fails as a *system*: crushed to pass 3:1,
`lit` and `dim` become near-identical browns and the distinction dies. Purple gives the light
ladder room while leaving the warm palette to mean "interactive". `lamp-out` sits deliberately at
1.79:1, below the 3:1 bar, because "off" should read as absent; its meaning is carried by the
accessible name and the visible state label, not the colour.

### The overlay invariant

> `hearth-light` must define **exactly** the same 17 colour token names as `hearth-dark`.

Light sits *on top of* dark. A colour present in dark but missing from light does not error — it
silently resolves to the dark value and leaks a near-black into a pale interface. Assert name
parity whenever either set changes.

A related trap when reading the file through the plugin API: an **inactive** set's tokens report
`resolvedValue` from whichever set *is* active. Reading `hearth-light` while Dark is active returns
dark hexes. Read `value`, not `resolvedValue`, for the inactive palette.

## Typography

**Fraunces** — variable, weights 100–900, optical sizing. Carries the entire interface.
**Departure Mono** — single weight 400, drawn on an **11-design-pixel em grid**. Wordmark and
deliberate emphasis only.

**Why the pixel face is restricted.** Departure Mono's outlines are all multiples of 50 units on a
550 upem, i.e. 11 design pixels per em. It renders crisply only at font-size multiples of 11px —
11, 22, 33. Any other size resamples it off its own grid, and small glyphs are where that shows
first. It also ships one weight, so it cannot express hierarchy at all.

Fraunces can. Moving the UI onto it makes **weight** a hierarchy axis alongside size and colour,
which is what lets small text stay legible where a one-weight pixel face could not.

**Weight tracks the ink ramp.** Within the 12px label tier — where nearly all of the interface
lives — weight is assigned from the text's own colour rather than chosen per-component:

| colour | weight | role |
|---|---|---|
| `ink` | 600 | primary values: names, field contents, telemetry readings |
| `ink-muted`, `tint-*` | 500 | controls, labels, links, game identity |
| `ink-faint` | 400 | metadata: timestamps, facts, badges, counts |
| `accent`, `danger`, `accent-ink` | 600 | semantic emphasis |

The point is **redundant encoding**: the same three levels are carried by colour *and* weight, so the
hierarchy survives greyscale, low vision, and any context where colour alone fails. It also means a
component never has to decide its own weight — the colour it already needed settles it.

Prose and headings (14px and above) keep their own weights; this rule governs the small tier only.

**The pixel face has its own scale, and it is not the prose scale.** Departure Mono renders
crisply only at multiples of 11px, so it uses exactly two steps — **22px** and **33px** — and no
others. These never mix with 12/14/16/20/24/30: the two scales belong to different faces and are
never compared, which is why the 1.125 ratio floor does not apply between them. 22px is the
default; 33px is for a wordmark that has to carry a page on its own.

Any pixel-face text at a size outside those two steps is a defect, not a variation.

Hierarchy: `display-3xl` (brand, page titles) → `display-xl` (section heads) → `display-base`
(card titles) → `prose-sm` (helper text, descriptions) → `label` (12px — controls, metadata and
logs alike).

**The small end of the scale is one step, not two.** An earlier 11px level was retired: every colour in the
palette was used at both 11px and 12px (`ink-faint` alone, 48 times at 11 and 36 at 12), so the two
sizes were one semantic level applied inconsistently — and at a 1.091 ratio the eye cannot separate
them anyway. Steps are now 12 → 14 → 16 → 20 → 24 → 30, ratios 1.167 / 1.143 / 1.250 / 1.200 /
1.250, none below the 1.125 floor at which a size difference stops being perceptible.

## Layout

**Spacing** steps in 2px, not 4px: 2, 4, 6, 8, 10, 12, 14, 16, 20, 24, 32, 40. This mirrors
Tailwind's half-steps, which the code uses heavily (`py-1.5`, `px-3.5`, `px-2.5`, `mt-0.5`). A 4px
grid would have broken parity with the shipped app, so the 2px grid is deliberate — do not "round
up to the nearest 4".

**Rhythm** — `gutter` 24px is the horizontal page padding and the standard inter-column gap;
`section` 20px separates stacked blocks; `block` 32px precedes a new section head. Rows are
24px horizontal / 16px vertical with a seam rule beneath.

**Whitespace** is tight by intent. This is an operations tool: more rows visible beats more air.

## Elevation & Depth

Almost none, by design. Separation is achieved with:

1. **Seam rules** — 1px `seam` lines between rows and around bands.
2. **Hairline borders** — 1px `edge` on controls.
3. **Surface shift** — `surface-raised` for panels, `seam` for inset wells.

The only true shadow is on the **toast** (a two-layer `shadow-lg` at 10% black), because it floats
over content. Do not add shadows elsewhere; use a seam or a border.

## Shapes

One radius: `control: 2px`. At typical control sizes this reads as a square corner with the
hardness taken off. The lamp is the single exception, a `pill` circle.

Borders are structural. A control's border *is* its affordance — the Secondary button has no fill
and no shadow, so its 1px `edge` border is the only thing that makes it a button.

## Motion

**The application does not move on its own.** Motion answers an action — a copy confirming, a
panel opening, a state changing — and shows what changed. Nothing animates because time passed.
This is deliberate: a tool watched while a server boots should not compete with itself for
attention, and the lamp glow was explicitly demoted once already for exactly that reason.

**The landing page is the single exception.** Its seam may carry ambient fire, because that page
is the product's front door rather than a working surface, and the hearth is the thing the whole
system is named for. The exception is bounded: one element on one page. Nothing else on the
landing animates unprompted — not the lamps, not the rail, not the worlds.

**State is never encoded in motion.** Lamp states are distinguished by *form* — solid disc, hollow
annulus, thin ring — so they survive greyscale, low vision, a frozen frame, and
`prefers-reduced-motion`. A reader who never sees a single frame of animation loses nothing but
atmosphere.

**Two animations exist only in code and cannot be drawn in Penpot.** Both are recorded here
because the design file will never show them:

- **The loading dot pulses** (`animate-pulse`) while a button's action is in flight. Penpot models
  the dot at rest. This is permitted by the rule above — it answers an action and reports that
  work is still running.
- **The lamp's glow**, handled the same way and deliberately demoted.

**Two dot sizes, on purpose.** The button's loading dot is 6px and takes the surrounding text's
colour, because it sits inline before a label and must match that text's optical weight. The
Toast's is 8px and always `{colors.accent}`, because it stands alone as the only status marker in
a floating notification with no label beside it to match. Same idea, different job — do not
unify them without changing both contexts.

**Under `prefers-reduced-motion` all ambient motion stops** and renders a single resting frame.
Action-driven feedback stays, because removing it would leave people unsure whether anything
happened.

## Components

**`Button`** — Variant × Size × State, 48 combinations.
- Variants: Primary (accent outline, fills on hover), Secondary (edge outline → accent on hover),
  Danger (edge outline → danger on hover), Link (no border).
- Sizes: Sm `6px 12px` @ 12px · Md `8px 14px` @ 12px · Lg `8px 16px` @ 14px.
- Disabled overrides every variant to `{colors.ink-faint}` on an `{colors.edge}` border at
  `opacity 0.55` — **including Link, which gains a border it otherwise never has**.
- Loading prefixes the label with a **6px dot that takes the variant's own text colour**, on the
  standard 8px gap. Unlike Disabled, Loading does *not* override the variant — a loading Primary
  stays accent, a loading Danger stays muted. The button is non-interactive while loading, but it
  keeps its identity, because the user is waiting for the action they chose and the control should
  still look like that action.
- In use the label also swaps to the in-flight verb — `Restart` becomes `Restarting`. The variants
  carry the neutral label because the verb is supplied per instance.
- Note the code declares 8 variants and 4 sizes but renders only 4 and 3: Success and Warning fall
  through to Primary, and `SizeXs` falls through to Sm. The design system models what renders.

**`Input` / `Textarea`** — State: Default, Focus, Disabled, Error. Field is `{colors.seam}` on a
1px `{colors.edge}` border. Focus adds a 2px `{colors.accent}` outline. Error swaps the helper line
for a `{colors.danger}` message. Label is 12px pixel `{colors.ink-muted}`; helper is 14px serif.

**`Lamp`** — State: Lit, Dim, Out, Unknown. 11.2px circle. Lit/Dim/Out are **filled discs**
separated by lightness; Unknown is the lone **hollow ring**. Colour alone does not carry the state:
`lit` vs `dim` separate by only 1.95:1 in light and 1.39:1 in dark, so form, the accessible name
and the visible state label are all load-bearing.

**`Toast`** — Kind: Ok, Err, Loading. `surface-raised` with a 1px kind-coloured border and the
system's only shadow. Loading carries a pulse dot.

**`Tag`** — Tone: Quiet, Accent, Alarm. Bare 12px pixel text, no chrome at all.

**`NavLink` / `TabLink`** — State: Active, Inactive, Hover. A label above a 2px rule; the rule is
`{colors.accent}` when active and transparent otherwise. They differ only in the gap above the rule
(4px nav, 8px tab).

**`StepBadge`** — State: Current, Past, Future. A 2-digit pixel number in a bordered box; Current
inverts to an accent fill.

**`OptionCard`** — State: Default, Selected, Hover. A bordered card; the border is the entire
selection signal (`edge` → `accent`). Selected and Hover are visually identical by design.

**Layout primitives** — `Row` (content + bottom seam rule), `RowList` (top rule + rows), `Band`
(rules above and below), `SectionHead`, `Prose`, `LogPane` (384px inset well), `TabBar`,
`StepIndicator`, `Nav`, `Drawer`.

## Do's and Don'ts

**Do**
- Bind every colour, spacing, radius, border width and font size to a token.
- Use `seam` for both inset backgrounds and divider rules — that duality is intentional.
- Keep Fraunces as the default; reach for the pixel face only for the wordmark or real emphasis.
- Let a 12px element's colour choose its weight, rather than setting weight per component.
- Give any status indicator an accessible name and a visible label; never let colour be the only
  signal.
- Add light values and dark values in the same change, or the overlay invariant breaks.

**Don't**
- Don't introduce a second radius. One control radius is the shape language.
- Don't fill buttons. Only Primary-on-hover is filled.
- Don't use the `tint-*` colours for interaction — they are per-game identity only.
- Don't point the lamp back at `accent`. They diverge in light for a measured reason.
- Don't add shadows. The toast is the only floating surface.
- Don't round spacing to a 4px grid; the system is 2px and matches the code.
- Don't rely on the lamp's glow for meaning — it is a dark-only enhancement.

## Responsive Behavior

Not extractable from the Penpot file — components were built at fixed widths. The code uses
Tailwind responsive prefixes in a few places (`sm:grid-cols-[2.5rem_1fr]` in the port rows) and
`flex-wrap` on the nav and row layouts, but the breakpoint system is not modelled in the design
file. See Known Gaps.

## Iteration Guide

1. **Start from the token sets, not the canvas.** `hearth-dark` holds all 41 tokens (17 colour +
   24 invariant). `hearth-light` holds 17 colour overrides and nothing else.
2. **Changing a colour** means changing it in both sets. Then assert both sets still list the same
   17 colour names.
3. **Adding a colour** means adding it to both sets in the same change.
4. **Changing spacing/type/radius** means changing it once, in `hearth-dark`. It is shared.
5. **After editing a token value, toggle the set** (or switch themes and back). Penpot does not
   repaint bound shapes when a value changes inside an already-active set — the value is correct
   but the canvas is stale until a toggle.
6. **Never move a token between sets.** Bound shapes keep the name, freeze at the last value, and
   stop tracking; re-applying the same name clears the binding outright. Add to the new set and
   leave the old one, or rename the set instead.
7. **Keep every type step at or above a 1.125 ratio.** Below that the difference stops reading as
   hierarchy and becomes noise. Adding 18px would put 18 → 20 at 1.111 and reintroduce the problem
   the 11px retirement solved; `sm:text-lg` should resolve to `text.xl` (20px) instead.
8. **Regenerate the Theme snapshot page** after any colour change. Those columns are detached
   copies with literal values — they do not follow tokens and will silently go stale.

## Known Gaps

- **No visual verification.** The Penpot instance's exporter is broken (`ERR_CONNECTION_REFUSED`
  reaching `penpot-frontend`), so `export_shape` was unavailable for the entire build. Every value
  here was verified numerically — geometry, contrast, token bindings, containment — but **no
  rendered output was inspected** beyond a single user-supplied screenshot of one dark button.
  The aesthetic prose above is grounded in measured values and the source CSS, not in viewed
  exports. Treat the written description as reasoned, not seen.
- **The pixel font in Penpot is a stand-in.** Components use **Space Mono**; the real face is
  **Departure Mono**, which the instance accepted as a custom font but would not render. The first
  stand-in, Silkscreen, was chosen on advance-width similarity and turned out to be **caps-only** —
  it silently drew every lowercase string as capitals, which misrepresented both the voice and the
  layout widths until a screenshot caught it. Replaced with **Space Mono**, which has true lowercase
  and sits within ~3.5% of Departure Mono's advance width. The `font.pixel` token names Departure
  Mono correctly.
- **`fontFamilies` tokens cannot be applied in Penpot 2.17.0.** Every value shape was rejected
  (`Field 1 is invalid: should be a set of strings`). Fonts are set directly on text shapes and
  carry no token binding — the one property in the system that is not token-bound.
- **A font family containing a space must be quoted inside the token value** (`"Departure Mono"`),
  or the resolver splits it on whitespace into `["Departure","Mono"]`.
- **Alpha-modified borders cannot be token-bound.** Applying a colour token resets `strokeOpacity`
  to 1, so `border-accent/40` (toast ok), `border-danger/60` (toast err) and
  `bg-surface-raised/80`, `bg-surface/40`, `border-edge/60` (step indicator) render at full opacity
  in the design file. The code retains the alphas.
- **Dark fails WCAG 1.4.11 for UI component boundaries.** `edge` against `canvas` is **1.50:1**
  against a 3:1 bar, and `edge` is the entire visible boundary of Secondary and Danger buttons.
  This predates the light theme. Light's equivalent is 3.01:1, so as things stand the light theme
  ships measurably more accessible than the dark one. Unresolved — tracked in AGR-7.
- **No accessible name on any lamp.** Every instance is an empty `<span>`; server status does not
  reach screen readers at all. Tracked in AGR-5.
- **Not modelled:** motion (the system has `transition-colors`, a 90ms checkbox tick and a
  `scale(0.97)` press, plus a `prefers-reduced-motion` block), breakpoints, the `.md` prose block
  for rendered mod READMEs, and the `.check` custom checkbox.
- **Light theme is unimplemented in code.** At the time of writing, agrelha has no `dark:` variants,
  no `prefers-color-scheme` handling, no theme toggle and no theme signal. This document specifies
  it; AGR-1 through AGR-8 implement it.
