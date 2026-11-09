# Kira — Design handoff

Desktop-first web app. These files are the **visual reference**, not production code.
Rebuild them as React + Tailwind components in `web/`; do not copy the inline styles verbatim.

## Files
| Path | What it is |
|---|---|
| `screens/*.png` | Pixel reference at 1440×900. **Match these.** |
| `screens/*.html` | Static HTML of each screen (open in a browser, inspect spacing/colors) |
| `tokens.css` | Colors, fonts, radii. Single source of truth → map into `tailwind.config` |
| `source/*.dc.html` | Original canvas source (only if you need exact values; needs the canvas runtime to render) |

## Screens → routes
| Screen | File | Route | PRD |
|---|---|---|---|
| Login | `login` | `/login` | FR-1, FR-2 |
| Dashboard | `dashboard` | `/` | FR-10, FR-13 |
| Entries + quick add | `entries` | `/entries` | FR-3, FR-6, FR-11 |
| Entry detail + history | `entry-detail` | `/entries/:id` | FR-4, FR-5, FR-6 |
| Export, Settings | not designed | `/export`, `/settings` | FR-7, FR-12, FR-13 — reuse the same shell, cards and table styles |

## Layout
- App shell: dark sidebar (`--sidebar`, 240px) + main content (`max-width: 1240px`, padding 32px 40px).
- Below ~800px the sidebar wraps above content (flex-wrap). No mobile-specific design.
- Cards: white, 1px `--border`, radius 14px, padding 20–24px.
- Tables scroll horizontally inside their card on narrow widths.

## Components to build (shared)
`AppShell`, `SidebarNav`, `LedgerStatus` (sidebar footer), `KpiCard`, `CategoryBars`, `TrendBars`, `EntriesTable`, `QuickAddBar`, `HistoryTimeline`, `VerifiedBadge`, `Button` (primary / outline / danger-outline), `Field` (label + input/select).

## Interaction rules (from PRD)
- `N` anywhere (not in an input) → go to `/entries` and focus the quick-add **Amount** field.
- Quick add Tab order: Date → Amount → Category → Payee → Paid with → Note → Receipt → Save. **Enter saves.** After save: clear amount/payee/note, keep date/category/method, refocus Amount, new row appears at top.
- Money always shown as `RM 1,234.56`, tabular numbers, `--font-display` for big figures.
- Voided rows: muted text + line-through. Amended entries: payee suffixed "(edited)".
- Receipt column: "Attached" (accent) / "Missing" (danger).
- "Correct entry" opens the same fields prefilled → creates an amendment. "Void…" asks for a reason (required).
- Ledger status in sidebar comes from `GET /api/ledger/verify`; if broken, replace with a red banner across the top of every page.

## Sample data
Numbers in the screens are placeholders. Real values come from the API (TRD §5).
