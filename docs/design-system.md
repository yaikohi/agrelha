# The hearth design system

agrelha's public and signed-in pages share one visual language, called *hearth*:
a warm dark panel, one ember accent, a soft display serif and a pixel mono.

Every token lives in exactly one place — the `@theme` block in
[`internal/web/assets/css/input.css`](../internal/web/assets/css/input.css).
Changing the palette means editing that block and nothing else.

## The rule

**Markup names the role it wants, never a colour.** Write `text-ink-muted`, not
`text-zinc-400` and not `text-[#9a8b7b]`. A token says what a thing *is* for, so
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
| `--color-edge` | `#3a2e26` | A visible border on a raised element; an unlit lamp |
| `--color-ink` | `#ede3d4` | Primary text |
| `--color-ink-muted` | `#9a8b7b` | Captions, secondary lines, quiet links |
| `--color-ink-faint` | `#6b5d51` | Present but incidental: version tags, colophon |
| `--color-accent` | `#ffa62b` | The ember. Live state, hover, focus, the primary action |
| `--color-accent-muted` | `#8a5a1e` | A banked ember: reachable but nobody there |
| `--color-accent-ink` | `#120e0b` | Text printed on an accent fill |
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

## Migration ledger

The system is being adopted page by page. These templates are migrated and are
checked by the ratchet:

- `dashboard.templ` — the branch between the two landing views
- `manager.templ` — the per-game managers (Valheim, Minecraft, Garry's Mod)
- `instance.templ` — the per-instance shell: breadcrumb, state, tabs, overview, console
- `gmod_instance.templ` — fully migrated; its three tabs are all shared ones
- `incident.templ`, `incidents.templ` — the last-failure panel and the crash backlog
- `log_pane.templ`, `host_banner.templ` — the shared log viewer and the unreachable-Host banner
- `nav_hearth.templ` — the signed-in navigation
- `panel.templ` — the public landing panel and the admin hub

`layout.templ` holds both shells and keeps `zinc` for `Layout` until the last
legacy page is gone. Still unmigrated: the Mods, Configs, Backups and Settings tab bodies inside
`valheim_instance.templ` and `mc_instance.templ` (their shells and shared tabs
are done), both create wizards, and `history`, `accounts`, `requests` and
`admins`. The `.md` block at the top of `input.css` styles rendered
README markdown and still carries its own greys; it migrates with the mod pages.
