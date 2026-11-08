# 10. Icons Are Pixelarticons, Rendered Only at 24px and 48px

Date: 2026-11-08

## Status

Accepted. Implements AGR-76 and supersedes the "prefer 16px inline" sizing note
that the Hearth design system carried before this decision.

## Context

Hearth had no iconography. Every affordance was carried by text or by the lamp,
and the one `icon()` helper rendered a remote mod thumbnail — content, not a
system.

Two things about the system had already decided the family. Departure Mono is a
pixel face and carries every label, count and button; `radius-control` is 2px, so
the whole system is drawn on a hard grid. A smooth vector family would sit beside
that as a visitor.

The open questions were which set, at what sizes, and by what mechanism.

## Decision

**Pixelarticons 2.4.2, default family only.** The release holds 1036 glyphs, of
which 332 have a `-sharp` twin and 53 a `-solid` one. A family covering a third of
the names cannot be the house family, and mixing two inside one screen is what
makes an icon set look assembled rather than drawn. MIT licensed, "Copyright (c)
2019 Gerrit Halfmann"; the notice is vendored with the glyphs because they are
embedded in the binary.

**Two sizes: 24px and 48px.** One grid unit must land on whole pixels. At 24px it
is one CSS pixel, at 48px two. At 16px it is 0.667 and at 12px 0.5, both of which
antialias a face whose point is hard edges. A pixel icon at a non-integer multiple
stops being a pixel icon.

**Inline SVG from vendored files, under `internal/web/components/icons`.** An
`<img>` cannot inherit `currentColor`, which the colour rule requires, and an
external `<use>` inherits it unreliably. The files sit in the components package
rather than under `assets/`, because they are component source data read at render
time, not assets served to a browser; `go:embed` cannot reach a parent directory,
and this keeps the leaf component from importing the web root for an embedded FS.

**The set is held to what the product renders.** A vendored glyph nothing uses
fails the build, as does a name nothing vendored. Growth is deliberate: add the
name to `ICONS` in the Taskfile, vendor it, use it.

## Consequences

`text-xs` rows get no icons — the tab bar, the telemetry line, the mod and
installed rows. That is the price of the size rule, and the remedy, if those rows
ever need icons, is a hand-drawn 16-unit subset rather than a fractional scale.

A 24px glyph inside a `py-1.5 text-xs` control makes that control taller. Only the
lifecycle controls and "Back up now" carry one; a button with an icon is a
decision, not a default.

Icons cannot carry meaning alone, so none of them is a state indicator. The lamp
stays the one status indicator, unchanged by this decision.

`warning-diamond` (112 path commands) and `bug` (98) were selected on paper but are
not yet vendored: both are busy enough to need a look on screen before they are
adopted, and nothing in the first adoption needed them.
