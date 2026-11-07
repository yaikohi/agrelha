# Spec: search UX — filters, sorting, and one results component

Status: **specified, not implemented.** Written 2026-11-07, decisions settled the same day.
Ready for the Plan phase.

## Assumptions I'm making

1. "All search functionalities" means the **six content-search surfaces** in the
   inventory below. The Valheim mod-config settings search
   (`web/handlers/valheim/configs.go`, `domain.SearchQuery` with
   text/section/changed) already has filters and truncation notices — it is the
   exemplar to copy, not a surface to change.
2. There is nothing to improve in logs, backups, access history or the instance
   list: none of them has a search box today, so adding one is a new feature, not
   a UX fix. Out of scope.
3. Datastar signals stay the state store. No URL-persisted query state, no
   server-side per-user filter persistence.
4. Hearth tokens and existing component classes (`components.InputClass`,
   `components.ButtonClass`) cover every new control. No new design tokens, and
   the Penpot spec is not re-opened for this.
5. Both catalogues stay as they are wired: Thunderstore is a full in-memory index
   (filtering and sorting are free, locally); Modrinth and CurseForge are remote
   per-query (sorting must be asked of them, or re-sorted locally over the merged
   page).
6. The deliverable of this phase is this document. No code until it is approved.

→ Correct me now or the Plan phase proceeds on these.

## Objective

Make mod and modpack search usable without guessing: see results as you type,
sort them, narrow them, and page through them — identically on every surface,
through one component instead of five hand-built markup blocks.

Users: the operator adding mods to a live world, and whoever runs the creation
wizard.

Success looks like: the operator finds a mod by what it does and how popular or
recent it is, never sees a deprecated or undownloadable mod presented as
installable, and is never left staring at an empty panel that only says
"Search Modrinth to find mods by name."

## What exists today

| # | Surface | Handler | Results target | Markup | Filters | Sort | Paging |
|---|---|---|---|---|---|---|---|
| 1 | Valheim instance mods | `valheim/detail.go:ValheimInstanceModsSearch` | `#valheim-mod-results` | `pages.ModSearchResults` | none | fixed: downloads | Load more (refetch, limit+24) |
| 2 | Minecraft instance mods | `minecraft/mods_search.go:MCInstanceModsSearch` | `#mc-mod-results` | `pages.ModSearchResults` | implicit mcVersion+loader facet | fixed: downloads | **none** (hard 20+20) |
| 3 | Valheim wizard mods | `valheim/wizard.go:ValheimWizardModsSearch` | `#wizard-valheim-mod-results` | **Go string HTML** | none | fixed: downloads | Load more (refetch) |
| 4 | MC wizard mods | `wizard/wizard.go:MCWizardModsSearch` | `#wizard-mod-results` | **Go string HTML** | version defaulted to `1.21.1` | fixed: downloads | **none** |
| 5 | MC wizard modpacks | `wizard/wizard.go:MCWizardModpacksSearch` | `#wizard-pack-results` | **Go string HTML** | none (API takes mcVersion, unused) | none | **none** (page 1 only) |
| 6 | Installed mods filter | client-side `$modFilter` | in place | `pages.InstalledMods` | substring | none | n/a |

Shared defects, independent of filters and sorting:

- **Nothing happens until you click.** Every surface requires Enter or a Search
  button press. No debounce, no live results.
- **Empty by default.** Surfaces 1 and 3 pre-load popular mods (`data-effect`
  with an empty query). 2, 4 and 5 open on a hint sentence.
- **Deprecated Thunderstore mods are offered as installable.**
  `rawPackage.IsDeprecated` is parsed into the index and then never read.
- **Three copies of a card.** Surfaces 3, 4 and 5 build their own cards as
  `fmt.Sprintf` HTML with hand-rolled JS escaping. None shows the
  installed/restricted badges or the Details drawer that the templ version has,
  and all three drift from it on every change. (Handler-side HTML is house style
  elsewhere — the problem here is the duplication, not the technique.)
- **MC results carry no metadata.** `minecraft.ModHit` has no downloads and no
  date, so `ModResultUI.Meta()` renders an empty line where Valheim shows
  author · version · downloads.
- **"No results" does not say why.** Surface 2 facets on the instance's exact
  mcVersion *and* loader — correctly — but a near-miss renders as
  `No mods found for "x" on Minecraft 1.21.1`, which reads as "no such mod"
  rather than "that mod does not run here".
- **Result counts lie by omission.** `N results for "q"` is the page size, not the
  match count, even though Modrinth returns `total_hits`.

## Scope of change

### One query type, one control row, one results component

```go
// domain
type ModQuery struct {
	Text       string
	Sort       ModSort // relevance | downloads | updated | name
	Provider   Provider // "" = all
	MCVersion  string   // "" = any
	Deprecated bool     // include deprecated/undownloadable
	Limit      int
	Offset     int
}
```

- `shared.ParseModQuery(c)` parses it once, from body or query string, keeping the
  existing per-surface field aliases (`q`, `modQuery`, `modSearch`,
  `wizardModSearch`, `packQuery`).
- `pages.ModFilterBar(ModFilterUI)` renders the control row: bound text input with
  `data-on:input__debounce.300ms`, a sort `<select>`, and only the toggles the
  surface supports. The Search button stays.
- `pages.ModSearchResults` grows `Mode` (`install` | `cart`) so surfaces 3 and 4
  render through it and their `fmt.Sprintf` blocks are deleted. Surface 5 gets
  `pages.ModpackResults`, the same shape in card form.
- Every surface echoes the live query back into the Load-more and action scripts,
  so paging and installing never silently drop the filters.

### Sort, per catalogue

| Sort | Thunderstore | Modrinth | CurseForge | ModpackIndex |
|---|---|---|---|---|
| relevance | match quality (name/owner, then description) | `index=relevance` | `sortField=2` | API order |
| downloads | local, index field | `index=downloads` | `sortField=2` | local, page only |
| updated | local, `UpdatedAt` | `index=updated` | `sortField=3` | not offered |
| name | local | local | `sortField=4` | local, page only |

Merged Minecraft results are re-sorted locally over the union of the two pages,
which requires `ModHit` to carry `Downloads` and `UpdatedAt`. Modrinth's
`SearchHit` already has `Downloads`; add `date_modified`. `domain.CFMod` already
has `Downloads`; add `dateModified` to `modDTO`. That plumbing also fills the
empty metadata line on every Minecraft card.

### Filters, per surface

| Surface | Filters exposed |
|---|---|
| 1, 3 Valheim mods | sort; **hide deprecated (default on)** |
| 2 MC instance mods | sort; provider (all / Modrinth / CurseForge); hide not-downloadable. **No version override:** the instance's version and loader stay a hard gate |
| 4 MC wizard mods | sort; version (bound to the wizard's `mc_version`); provider when CurseForge is wired |
| 5 MC modpacks | sort; version (bound to `mc_version`, "any" allowed); Load more by page |
| 6 Installed | sort (name / version); keeps the substring filter |

### Compatibility stays a gate, not a filter

The version and loader facet is never loosened. A mod that cannot load is not a
result, so no toggle offers it and no card ever renders it as installable.

What changes is the empty state. When a search returns nothing, re-run the
Modrinth query once with the version facet dropped, read `total_hits`, and say so
without listing the mods:

> No mods named "create" run on Minecraft 1.21.1 with NeoForge. 7 mods match that
> name on other versions.

That is one extra request, only on the zero-result path, and it answers the
operator's real question: does the mod not exist, or does it not fit this world?

"Compatible" here means version and loader only. Server-side support is a
different gate, specified in `docs/mod-compatibility-plan.md` and out of scope.

### Defaults and headers

- Empty query means "popular", pre-loaded on open, on all five catalogue
  surfaces — the behaviour surface 3 already has.
- Sort defaults to `relevance` with a query, `downloads` without one.
- One header sentence everywhere: `Showing 20 of 1,243 · sorted by downloads`,
  with the count from `total_hits` where the provider gives one and an explicit
  "more available" where it does not. Truncation is stated, never silent.

## Out of scope

Category and tag filters (Thunderstore categories are not even parsed into the
index, and Modrinth facets would need real plumbing); saved filter presets;
fuzzy/typo-tolerant matching; URL-shareable search state; infinite scroll;
keyboard navigation of results; search on logs, backups, access history or the
instance list.

## Tech stack

Go 1.25 · Fiber v2 · templ · Datastar 1.0.2 · Tailwind (hearth tokens). No new
dependencies.

## Commands

```
Generate:   task generate        # templ + tailwind
Build:      task build
Test:       task test
Fast tests: task test:fast
Coverage:   task test:cover
Vet:        task vet
Dev:        task dev             # :7331 proxy, :8080 app
```

## Project structure touched

```
internal/domain/mod.go                      → ModQuery, ModSort
internal/ports/game.go                      → PackageCatalog.Search takes ModQuery
internal/infra/content/thunderstore/         → local filter + sort over the index
internal/infra/content/modrinth/             → index param, date_modified
internal/infra/content/curseforge/           → sortField/sortOrder, dateModified
internal/infra/content/modpackindex/         → mcVersion + page already supported
internal/web/shared/                         → ParseModQuery
internal/web/pages/mods.templ, mods.go       → ModFilterBar, Mode, ModpackResults
internal/web/pages/{valheim,mc}_instance.templ, {valheim,mc}_wizard.templ
internal/web/handlers/{valheim,minecraft,wizard}/
internal/wiring/server.go                    → merged search passes ModQuery
```

## Code style

Match the surrounding code. One worked example of the shape intended, from the
Thunderstore client:

```go
func (c *Client) Search(_ context.Context, q domain.ModQuery) ([]SearchResult, error) {
	c.mu.Lock()
	idx := c.index
	c.mu.Unlock()

	text := strings.ToLower(q.Text)
	out := make([]SearchResult, 0, q.Limit)
	for _, p := range idx {
		if p.IsDeprecated && !q.Deprecated {
			continue
		}
		if text != "" && !matches(p, text) {
			continue
		}
		out = append(out, p)
	}
	sortResults(out, q.Sort)
	return page(out, q.Offset, q.Limit), nil
}
```

Conventions: no new code comments; exported identifiers keep their existing doc
comments; no new abstraction layers — `ModQuery` replaces loose parameters rather
than wrapping them.

## Testing strategy

`go test` table tests beside the code, as now
(`mods_search_test.go`, `handlers_edge_test.go`, `client_test.go`).

| Level | What |
|---|---|
| domain | `ModSort` parsing and its defaults (empty query → downloads) |
| infra | Thunderstore: deprecated excluded unless asked, each sort order, offset paging. Modrinth/CurseForge: the request carries the mapped sort param |
| wiring | merged MC results come back in the requested order; one catalogue failing still returns the other |
| handlers | the control row renders with the current query echoed; Load more preserves filters; header states the real total |
| pages | `Mode: cart` renders Add/Remove, `Mode: install` renders Install/Remove |

No coverage target beyond "every new branch has a case".

## Boundaries

- **Always:** `task generate` after touching `.templ`; `task test` and `task vet`
  before declaring done; keep SSE patch selectors unchanged; escape through templ
  rather than by hand.
- **Ask first:** changing `ports.PackageCatalog` (it has test doubles across
  packages); deleting the handler-side `fmt.Sprintf` card markup; any change to
  install/cart POST payload shapes.
- **Never:** commit or push (the user commits); add a dependency; widen a
  catalogue's default page size without a paging control; present a deprecated or
  undownloadable mod with a working Install button.

## Success criteria

1. Typing in any of the five catalogue search boxes updates results ~300 ms after
   the last keystroke, with no click.
2. All five open with popular results already listed.
3. Sort is selectable on all five and the rendered order matches the selection,
   including across merged Modrinth + CurseForge results.
4. Deprecated Thunderstore mods are absent by default and reachable behind one
   toggle.
5. A Minecraft search with no compatible match states the version and loader it
   filtered on, and how many mods of that name exist elsewhere. It offers none of
   them.
6. Every Minecraft result card shows author · downloads, like Valheim's.
7. Load more exists on all five, appends, and preserves the active filters.
8. Mod and modpack card markup exists exactly once, in templ: the three
   `fmt.Sprintf` card builders in `valheim/wizard.go` and `wizard/wizard.go` are
   gone. Other handler-side HTML (`configs_render.go`, backups, console) is house
   style and is left alone.
9. `task test` and `task vet` pass.

## Progress

**Slice 1 and 2 (Valheim): done 2026-11-07.** `go test ./...` and `go vet ./internal/...` pass.

| Added | Where |
|---|---|
| `ModSort`, `ModQuery`, `ModSearchPage` | `internal/domain/mod.go` |
| `Search(ctx, ModQuery) (ModSearchPage, error)` | `internal/ports/game.go`, one adapter and two test doubles updated |
| Local filter, relevance scoring, four sorts, deprecated gate | `internal/infra/content/thunderstore/client.go` |
| `ParseModQuery` | `internal/web/shared/modquery.go` |
| `ModSearchControls`, `ModSearchScript` | `internal/web/pages/mods.templ`, `mods.go` |
| Honest heading and empty state | `internal/web/handlers/valheim/search.go` |
| Tests | `domain/modquery_test.go`, `thunderstore/search_test.go`, `shared/modquery_test.go`, `valheim/search_test.go`, `pages/mod_search_controls_test.go` |

Three deviations from the spec above, all deliberate:

1. **Slice 2 came with slice 1.** A sort no one can select is not a feature, so
   the control row, the 300 ms debounce and the popular-on-open default landed
   with the backend rather than after it.
2. **`ModSearchPage` instead of a bare slice.** Carrying the match total fixed the
   "counts lie by omission" defect in the same pass and made Load more exact
   instead of a `len(results) >= limit` guess. No `Offset` field was added: the
   in-memory index makes a wider page free, and Minecraft will need offsets in
   slice 3 anyway.
3. **Filters travel as Datastar signals, not server-echoed state.** Every trigger
   posts `q: $modQuery, sort: $modSort, deprecated: $modDeprecated`, so Load more
   and the next search cannot drift from what the controls show.

## Slices

1. ~~`ModQuery` + `ParseModQuery` + Thunderstore local filter/sort. Surfaces 1 and
   3 get sort and the deprecated filter.~~ **Done.**
2. ~~`ModFilterBar` + debounce + popular-by-default, applied to 1 and 3.~~
   **Done, with slice 1.**
3. `ModHit` metadata and sort plumbing for Modrinth/CurseForge; surface 2 gets
   sort, provider, any-version, Load more.
4. Surfaces 3, 4, 5 move onto the shared templ; delete the `fmt.Sprintf` markup.
5. Modpack paging and version filter; installed-list sort.

## Decisions (resolved 2026-11-07)

1. **Slice order stands.** Valheim first, because the in-memory index makes it the
   cheapest end-to-end proof of `ModQuery`, the filter bar and the results
   component. Minecraft's missing paging is slice 3.
2. **No version override on Minecraft search.** An incompatible mod is not a
   result. The fix is the explanatory empty state above, not a filter that offers
   mods the world cannot load.
3. **`ports.PackageCatalog.Search` takes `domain.ModQuery`.** One signature, not
   two: the port speaks the domain's language and each infra adapter translates
   `ModQuery` into its provider's parameters (`index=`, `sortField=`, or a local
   sort over the index). A second query-taking method beside the old one would put
   two ways to say one thing in the port, and the cost it avoids is not real —
   `PackageCatalog` has one implementation (`infra/content/thunderstore`) and two
   test doubles (`handlers/content/content_test.go:33`,
   `handlers/valheim/valheim_test.go:67`).
