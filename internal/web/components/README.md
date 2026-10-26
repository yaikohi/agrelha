# Web components (`internal/web/components`)

The hearth design system's primitives. Every visual atom lives here, one component
per file, and nothing in this package knows about a page's view model — components
take plain strings, bools and small structs defined alongside them.

## The rule

Components name **roles**, never colours: `text-ink-muted`, not `text-zinc-400` and
not `text-[#9a8b7b]`. The tokens are defined once in
`internal/web/assets/css/input.css` and documented in
[docs/design-system.md](../../../docs/design-system.md).
`pages/design_tokens_test.go` globs every `.templ` in this package and in `pages`
and fails the build on a Tailwind default-palette class or a raw hex.

## Files

| File | What it gives you |
|---|---|
| `button.templ` | `Button`, plus `ButtonClass` / `InputClass` for markup that cannot take a component |
| `card.templ` | `SectionHead`, `Prose`, `RowList`, `Row`, `Band`, `Drawer` — the layout language |
| `input.templ` | `Input`, `Textarea` with label, helper and error text |
| `lamp.templ` | `Lamp`, `LampBound` — the one status indicator |
| `log_pane.templ` | `LogPane`, the follow/pause log viewer |
| `nav.templ` | `Nav`, the site navigation |
| `nav_link.templ` | `NavLinkClass`, `TabLinkClass`, `QuietLinkClass` |
| `option_card.templ` | `OptionCard` for wizard pickers |
| `step_indicator.templ` | `StepIndicator` for wizard progress |
| `tab_bar.templ` | `TabBar` |
| `tag.templ` | `Tag` — quiet, accent and alarm text tags |
| `toast.templ` | `Toast`, bound to the `$toast` / `$toastkind` signals |

## Variants

`VariantPrimary` and `VariantSuccess` render the accent outline. `VariantSecondary`,
`VariantGhost` and `VariantLink` stay quiet. **Only `VariantDanger` reads as
destructive** — stopping a world is not destructive, and colouring it like Delete
teaches people to ignore the warning. A test enforces this.

## Import invariants

May import: stdlib, `github.com/a-h/templ`, and `internal/domain` (the nav needs the
registered game profiles). Must never import `internal/web/pages`, `internal/app`,
`internal/ports` or `internal/infra`.
