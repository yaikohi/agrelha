# Web Components (`internal/web/components`)

This package contains pure, reusable presentation primitives for Agrelha, built with Go and `a-h/templ`.

## Architectural Role
- **Design System Primitives**: Provides typed UI atoms (`Button`, `Input`, `Textarea`, `TabBar`, `StatusPill`, `OptionCard`, `Modal`).
- **Strict Functional Color Language**:
  - **Monochrome Zinc**: Navigation tabs, card outlines, form controls, primary CTA actions, and neutral elements.
  - **Emerald (Green)**: Server running states and `Start Server` actions.
  - **Amber (Orange)**: Server starting/booting states and `Stop Server` actions.
  - **Red**: Error alerts and destructive actions (`Delete World`, remove mod).
- **Zero Runtime Overhead**: Templates compile into streaming Go bytecode writing directly to an `io.Writer`.

## Import Invariants
- May import: stdlib (`fmt`, `strings`, `strconv`) and `github.com/a-h/templ`.
- Must never import: `internal/infra`, `internal/app`, or `internal/ports`.

## Exported Components

### 1. `Button(props ButtonProps)`
Polymorphic button / anchor tag with variants and sizes:
- Variants: `VariantPrimary` (high-contrast monochrome), `VariantSecondary`, `VariantSuccess` (start), `VariantWarning` (stop), `VariantDanger` (delete), `VariantGhost`, `VariantLink`.
- Sizes: `SizeXs`, `SizeSm`, `SizeMd`, `SizeLg`.
- Supports Datastar actions (`OnClick`, `Indicator`), disabled states, and download links.

### 2. `Input(props InputProps)` & `Textarea(props TextareaProps)`
Form controls with high-contrast neutral focus rings (`focus:border-zinc-400 focus:ring-1 focus:ring-zinc-400`):
- Supports label, helper text, error text, required indicator, and Datastar bindings (`Bind`, `OnKeyDown`, `OnChange`).

### 3. `TabBar(props TabBarProps)`
Standardized horizontal tab navigation bar:
- Active tab renders with `border-b-2 border-zinc-100 text-zinc-100 font-semibold`.
- Inactive tabs render with `text-zinc-400 hover:text-zinc-200`.

### 4. `StatusPill(props StatusPillProps)`
Standardized status badge with pulsating or static dot:
- Supports static server-rendered states (`"running"`, `"stopped"`, `"starting"`, `"offline"`, `"active"`, `"disabled"`) and live Datastar signal-driven reactive states (`Signal: "online"`).

### 5. `OptionCard(props OptionCardProps)`
Selectable card for wizard and resource tier pickers:
- Highlights selected item via Datastar: `border-zinc-300 ring-1 ring-zinc-300 bg-zinc-800/60`.
- Supports icons, badges, titles, subtitles, and descriptions.

### 6. `Modal(props ModalProps)`
Structured dialog panel controlled by a Datastar boolean signal (`OpenSignal`):
- Includes header with title, subtitle, and close button (`data-on:click="$signal = false"`).

### 7. `BudgetBar(props BudgetBarProps)`
Cluster capacity and quota visualization bar:
- Displays RAM usage ratio (`UsedGiB / TotalBudgetGiB`) with proportional horizontal progress bar via `RAMPercent()`.
- Displays running instance counts (`RunningCount / MaxRunning`) and saved instance counts (`TotalInstances / MaxInstances`).
- Optional secondary action button (`ActionHref`, `ActionText`) for direct navigation to access or management settings.

### 8. `EmptyState(props EmptyStateProps)`
Dashed placeholder container when no records or instances exist:
- Centered layout with optional icon/emoji badge, title heading, and description body.
- Optional primary CTA `@Button` (`ActionHref` or `ActionClick`) and support for templ children blocks.

### 9. `StepIndicator(props StepIndicatorProps)`
Horizontal multi-step progression indicator for wizards:
- Takes a slice of `StepItem{Number: int, Label: string}` separated by divider lines.
- Dynamically highlights the active step using Datastar reactive signals (`data-class` binding against `$step`).
