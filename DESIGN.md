# DESIGN.md — VideoCMS frontend

Visual system for the Nuxt frontend (`videocms-frontend/`). Register: **product** (calm ops console). Backend player views are out of scope.

## Theme

Light and dark, both first-class. Dark is tuned for an evening self-hoster checking a server; light for daytime desk work. Theme is toggled via `useTheme()` (sets `data-theme` on `<html>`, persisted in a cookie, falls back to `prefers-color-scheme`). Tokens live in `videocms-frontend/assets/css/main.css` as DaisyUI 5 theme blocks in OKLCH.

## Brand mark

The real logo is `videocms-frontend/public/logo.png` (coral "W / CMS" mark, transparent bg), used at 28–48px in the navbar, sidebar, footer, and login — always paired with the app name, never tiled or recolored. Note the deliberate tension: the logo is coral (~hue 22) while the UI accent is indigo; a coral UI accent was rejected because it would collide with error-red on destructive actions. Revisit only as a whole-palette decision.

## Color

Strategy: **Restrained**. Indigo (hue 277) is the only accent and means action / selection / live state. Neutrals are tinted toward indigo at chroma 0.002–0.03. Semantic colors are reserved for state.

| Role | Light | Dark | Use |
|---|---|---|---|
| `base-100` | `oklch(99.5% .002 277)` | `oklch(21% .015 277)` | raised surface (cards, panels, sidebar) |
| `base-200` | `oklch(97.3% .004 277)` | `oklch(17% .013 277)` | page background |
| `base-300` | `oklch(91.5% .008 277)` | `oklch(28% .018 277)` | hairline borders, recessed fills |
| `base-content` | `oklch(24% .03 277)` | `oklch(91% .01 277)` | ink |
| `primary` / `accent` | `oklch(51% .23 277)` | `oklch(58.5% .2 277)` | actions, selection, focus |
| `info/success/warning/error` | see main.css | see main.css | state only |

Secondary text: `text-base-content/70` minimum. `/50` is reserved for disabled. Never gray text on colored backgrounds.

Dark mode depth comes from surface lightness (page 17% → card 21% → border 28%), not shadows.

## Typography

One family: the system sans stack (Tailwind default `font-sans`). Fixed rem scale, ~1.2 ratio:

| Role | Class | Notes |
|---|---|---|
| Page title | `text-xl font-semibold tracking-tight` | one per page, via `<PageHeader>` |
| Section / card title | `text-sm font-medium` | plain case — **no uppercase tracked eyebrows** |
| Body / table | `text-sm` | |
| Metadata / captions | `text-xs text-base-content/70` | |
| Metric values | `text-2xl font-semibold` | proportional figures on standalone stats |
| Aligned numbers | add `tabular-nums font-mono`-free `tabular-nums` | table columns, axis ticks only |

## Spacing, radius, elevation

- 4pt scale (Tailwind). Card padding `p-4`/`p-5`; section separation `gap-6`/`gap-8`; sibling grouping `gap-2`/`gap-3`.
- Radius: cards `--radius-box` 12px, fields/buttons 8px, selectors 6px. Nothing rounder than 16px except pill badges.
- Elevation: **1px `base-300` border, no decorative shadows.** Overlays (modals, dropdowns) may use one soft shadow ≤ 8px blur. Never border + wide shadow together.
- Z-index: semantic vars `--z-dropdown … --z-tooltip` in main.css. No arbitrary 999s.

## Components

- **Card**: `bg-base-100 border border-base-300 rounded-box`. No `shadow-xl`, no nested cards, no side-stripe accents.
- **PageHeader** (`components/PageHeader.vue`): title + optional description + actions slot. Every panel page starts with one.
- **Buttons**: DaisyUI `btn`; primary indigo for the page's main action only, `btn-ghost`/`btn-outline` elsewhere. One save-button look everywhere.
- **Tables**: DaisyUI `table` + `table-sm`, muted header row, `tabular-nums` on numeric columns, row hover `hover:bg-base-200/60`.
- **Loading**: skeletons shaped like the content (not centered spinners). On refetch, hold the previous render at reduced opacity — no skeleton flash.
- **Empty states**: icon + one plain sentence + the action that fills the state.
- **Modals**: native `<dialog>` via DaisyUI `modal`; used only when inline/progressive disclosure won't do.

## Charts (ApexCharts)

Single source: `composables/chartTheme.ts` (`useChartBaseOptions()`, `useChartPalette()`, `areaFill`, `chartTimeRanges`).

- Categorical slots, fixed order, validated for CVD/contrast in both modes: **1 indigo `#4f46e5`/`#6367ef` · 2 teal `#0d9488` · 3 amber `#d97706`**. Never cycle past 3; fold into "Other".
- Single-series charts use slot 1. Ranked/nominal bars: **one hue for all bars** (no value-ramps, no donuts, no rainbow `palette4`).
- Grid: solid hairlines (`strokeDashArray: 0`), muted labels, no toolbar/zoom.
- Lines 2px; area fills are a wash (0.18 → 0.02), never a saturated block.
- ≥2 series → legend shown; text in ink tokens, never series color.
- One time-range control scoping the charts below it (`chartTimeRanges` presets), not one per card.
- Status colors mark state (queue overload, errors) and are never used as series identity.

## Motion

150–250ms, `--ease-out`, state changes only (no page-load choreography, no decorative animation). Page transition: 150ms fade. `prefers-reduced-motion` collapses all transitions (global rule in main.css).

## Voice

Plain technical labels: "Storage", "Delivery traffic", "Encoding queue". No marketing tone in the panel, no invented statuses, no emoji in UI copy.
