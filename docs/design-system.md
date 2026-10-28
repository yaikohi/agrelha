# The hearth design system

agrelha's public and signed-in pages share one visual language, called *hearth*:
a warm dark panel, one ember accent, a soft display serif and a pixel mono.

Every token lives in exactly one place — the `@theme` block in
[`internal/web/assets/css/input.css`](../internal/web/assets/css/input.css).
Changing the palette means editing that block and nothing else.

## The rule

**Markup names the role it wants, never a colour.** Write `text-ink-muted`, not
`text-zinc-400` and not `text-[#c2b1a0]`. A token says what a thing *is* for, so
a later palette change does not have to guess which `zinc-800` meant "divider"
and which meant "raised surface".

`internal/web/pages/design_tokens_test.go` enforces this on every migrated
template and fails the build on a Tailwind default-palette class or a raw hex.

## Tokens

| Token | Value | Role |
|---|---|---|
| `--font-display` | Fraunces | World names, prose, headings. Warm and slightly wonky |
| `--font-pixel` | Departure Mono | Addresses, counts, labels — anything a machine produced |
| `--color-surface` | `#1e1713` | The page ground |
| `--color-surface-raised` | `#2a211b` | Anything sitting above the ground: inputs, wells, menus |
| `--color-seam` | `#120e0b` | The line between two ports; recessed areas |
| `--color-edge` | `#42352c` | A visible border on a raised element; an unlit lamp |
| `--color-ink` | `#ede3d4` | Primary text |
| `--color-ink-muted` | `#c2b1a0` | Captions, secondary lines, quiet links |
| `--color-ink-faint` | `#9c8b7c` | Present but incidental: version tags, colophon |
| `--color-accent` | `#f4df75` | The ember. Live state, hover, focus, the primary action |
| `--color-accent-muted` | `#efb268` | A banked ember: reachable but nobody there |
| `--color-accent-ink` | `#17110e` | Text printed on an accent fill |
| `--color-danger` | `#d9594c` | Destructive actions only — never a status |
| `--color-tint-sky` | `#7fa6b8` | Game identity, carried from the profile's accent |
| `--color-tint-moss` | `#8fa860` | Game identity |
| `--color-tint-flame` | `#d9733a` | Game identity |
| `--radius-control` | `2px` | Buttons and inputs. Nothing else is rounded |

## Components defined in CSS

Two things are CSS classes rather than utilities, because they carry a glow that
utilities express badly:

- `.lamp` with `.lamp-lit` / `.lamp-dim` / `.lamp-out` / `.lamp-unknown` — a
  port's state. **Lit** means people are playing, **dim** means it is up and
  empty, **out** means it is not running, and **unknown** is an empty socket:
  the Host could not be reached, so agrelha is not claiming either way. The lamp
  is the entire status system; nothing else repeats it.
- `.focus-accent` — the visible keyboard focus ring. Every interactive element
  carries it.
- `.check` — a checkbox. The browser's own control ignores `background` and
  `border` once `accent-color` is set, so an unchecked box renders white on the
  dark panel. This draws the box from tokens and marks the checked state with an
  accent tick rather than an accent fill: the config editor can show two hundred
  of them at once, and that many filled embers would stop the colour meaning
  "live".

## One filled accent per page

The accent is the ember, and the lamp owns it. At most one element on a page may
be *filled* with it — the page's single primary action, such as Join on the
public panel. Where several actions are peers, as with one manager button per
game on the hub, they are outlined instead. Three solid accent blocks on one
page stop the colour meaning "live".

## What is not a token

`--color-danger` is the only status colour, and it marks *actions*, not state.
State is the lamp. Resist adding a success or warning colour: a second
status colour would mean the lamp had stopped being trusted.

## Where it lives

Tokens: the `@theme` block in
[`internal/web/assets/css/input.css`](../internal/web/assets/css/input.css).

Components: [`internal/web/components`](../internal/web/components/README.md), one
per file, each taking plain values rather than page view models.

Page-level composites — a port, a manager row, an instance header — stay in
`internal/web/pages`, because they take view models and compose the primitives.

## Coverage

Every `.templ` in `pages` and `components` is checked by
`pages/design_tokens_test.go`: it globs both directories, so a new template is
covered the moment it exists, with no ledger to keep up to date.

It also checks that the values in the table above still match `input.css`. The
accent row drifted once and nobody noticed, which is the whole argument for the
check.

Some handlers build HTML as Go strings, because the fragment is patched in over
SSE rather than rendered with the page. The glob above could not see them, so
they sat on the pre-hearth palette through an entire redesign.
`TestHandlerMarkupNamesRolesNotColours` now walks every hand-written `.go` file
under `internal/web` as well, with a `notYetMigrated` list for the files still
carrying the old colours. That list may only shrink: a file on it that has been
migrated fails too, so the only way to silence the test is to finish the job.

The `.md` block in `input.css` styles rendered mod READMEs — the one place
agrelha styles elements it did not emit, so it cannot put a class on them. It
reads the same tokens through `var()`.

### Reusing a control's classes outside templ

`ButtonClass`, `InputClass` and `TextareaClass` in `components` return the class
string on its own, so markup assembled in Go gets the same control as markup
written in templ. Reach for those before writing out a border and a background
by hand.
