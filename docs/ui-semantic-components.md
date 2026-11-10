# Semantic UI components and visual contracts

This is the cross-page component inventory and target visual contract for OLR.
Read it with the [visual guide](ui-style-guide.md) and
[content guide](ui-content-guide.md). Heading, MetricValue, PropertyRow,
TechnicalValue, the Card family and shared surface utilities implement the
core roles. The inventory distinguishes these from future extractions.

## Audit scope and findings

Reviewed the route registry, global theme, shared UI/layout components and
presentation patterns across Overview, device/group details, Gateway/interfaces,
DNS/DHCP, Firewall, Access services, Tools/results, Advanced settings,
authentication/setup and system feedback. Wireless reads one managed access
point and uses the same empty-state contract; it has no configuration editor.

The project already has good foundations: Geist, semantic theme colors, shared
controls, PageHeading/SubPage, Card, ListRow, Dialog and tabular traffic values.
The original audit found differences in role definitions and local overrides:

| Evidence | Original difference | Shared decision |
| --- | --- | --- |
| `components/ui/card.tsx`, `index.css` | Card uses `rounded-xl` (16px), although the guide says panels are 12px | Standard panels are 12px; fix the shared radius mapping/use in an implementation pass |
| `routes/overview.tsx`, `features/network-quality/network-quality.tsx`, `components/ui/card.tsx` | Monitoring headings are 14px, normal CardTitle is 16px | Explicit compact monitoring heading and normal panel heading roles |
| `routes/gateway/interfaces.tsx`, `routes/gateway/interface.tsx` | Section headings use local 18px medium styles | Shared section heading defaults to 16px semibold |
| `routes/tool-page.tsx`, `components/traffic-metrics.tsx` | Tool results use 30px monospace; rates use 14px sans semibold | One metric role with inline, summary and result emphasis; measurements use tabular sans |
| `features/devices/device-inline-editor.tsx` | Entity title grows to 30px on desktop | Page/entity title defaults to 24px; identity comes from imagery and placement |
| `features/devices/device-setting-row.tsx`, `features/link/network-status.tsx` | Label/value patterns exist as local components with different text sizes | Reusable property rows and technical-value role |
| `components/layout/settings-list.tsx`, `components/ui/list.tsx` | Settings answers inherit subdued trailing-text styling | Distinguish an answer value from secondary metadata |
| `routes/access.tsx`, `routes/advanced/index.tsx`, interface cards | Several locally composed interactive surfaces | One launcher (`components/layout/launcher.tsx`) for both directories; panels keep their own layouts |

The table records the original audit. Its radius, heading, measurement,
property-value and interaction differences are addressed by the shared contracts
below. Specialized chart layouts retain their documented exceptions.

## Component inventory

Do not make a separate React component for every text size. Extract components
where structure, behavior or repeated semantics need a shared owner. Text roles
can be shared variants/tokens within those components.

| Semantic component | Responsibility | Existing owner / next step |
| --- | --- | --- |
| Page frame | Width, gutters, navigation, page spacing | Reuse AppShell and SubPage |
| Page heading | h1, optional context, nested breadcrumb, action | Reuse PageHeading; converge local entity headings visually |
| Section heading | Heading plus optional action/context | Heading; choose `as` independently of `size` |
| Panel | Static information group with header/content/footer | Reuse Card family; unify radius and heading variants |
| Navigation tile | A capability/entity with one primary destination | LauncherTile in `components/layout/launcher.tsx`; logo, initial or icon mark, optional state line |
| Property list / row | Label and current answer, optionally an edit trigger | PropertyRow inside a `dl`; DeviceSettingRow re-exports the same component |
| Metric value | Measurement and optional unit | MetricValue, used by Tools, traffic primitives, Gateway and DNS summaries |
| Traffic meter | Current rate, optional limit and utilization | Reuse Meter; keep data/direction semantics in traffic code |
| Status | A state's tone and its one word | `state()` in `lib/state.ts` owns the words and their tones; `StatusPill`, `StatusInline` and `StatusDot` in `components/ui/status.tsx` draw them; `StatusStrip` owns a module verdict. Badge remains a tag primitive |
| Row action | One trailing action, or a menu of several | `RowAction`, `RowMenu` and `RowChevron` in `components/ui/row-action.tsx`; ListRow renders the pencil for `editLabel` rows |
| Technical value | IP, MAC, hostname, interface identifier, command | TechnicalValue for wrapping identifiers; literal output retains pre/code |
| List / table | Repeated entities or comparable records | Reuse List/ListRow and Table; right-align numeric columns |
| Form field | Label, control, hint, validation and feedback | Compose existing Label/Input/Select/Switch/EditableField; share repeated field structure |
| Disclosure | Optional detail without changing the main answer | Reuse Disclosure; dialog-based Overview details retain their existing behavior |
| Dialog / menu / tooltip | Modal editing, contextual actions, short supplemental help | Reuse current primitives; never make tooltip-only information necessary for touch users |
| Feedback / empty state | Loading, error, no data, completion and retry | Reuse Skeleton, Alert, ListEmpty and Sonner; generalize empty-state layout only when needed |
| Chart frame | Title/actions, plot space, legend, data availability | Share frame conventions; keep radar, map, activity matrix and Sankey domain-specific |

## Size variants

Size is explicit and independent of semantic HTML, state and elevation. Choose
by information role; avoid local font-size overrides or making every component
support every size.

| Component | Small | Medium / default | Large |
| --- | --- | --- | --- |
| Button | `sm`: 32px desktop / 44px phone | `default`: 36px desktop / 44px phone | `lg`: 44px |
| Switch | — | 44 × 24px at every size; `busy` and `disabled` are different states | — |
| StatusPill | — | 24px tall, 12px medium text, no dot; one per surface | — |
| Heading / CardTitle | `sm`: 14/24px, compact monitoring/list group | `md`: 16/24px, section/panel (default) | `lg`: 24/32px, page/entity title |
| MetricValue | `sm`: 14/20px, inline (default) | `md`: 20/28px, summary | `lg`: 30/36px, diagnostic result |
| Card | `sm`: 12px phone / 16px desktop spacing | `default`: 16px / 24px spacing | No extra large-card density; layout controls width |
| PropertyRow | `sm`: 32px desktop minimum | `md`: 36px desktop minimum (default) | Wrapped content grows naturally |

Pass these through `size`. Every PropertyRow size retains 44px on phones. Card
density does not resize its title. Heading defaults to h2; `as` chooses document
hierarchy independently. DialogTitle retains its accessible primitive and uses
the medium heading scale.

`<MetricValue size="lg" value="23.4" unit="ms" />` displays a test result;
`<MetricValue value={formatRate(rate)} />` displays an inline rate. A null or
undefined value displays an accessible unavailable dash without a unit. Keep
observed zero as zero. PropertyRow children supply any actual editing controls.

ListRow titles share one treatment — 14px foreground text — so a list stays
readable when only some rows have a value. `trailingTone="value"` carries the
emphasis instead: the setting's answer is medium weight in the foreground. The
default `muted` variant remains for auxiliary metadata.

## Surface and interaction contract

| Surface | Shape and depth | Spacing | Interaction |
| --- | --- | --- | --- |
| Standard panel | 12px radius, 1px `border`, `bg-card`, `shadow-xs` | 16px phone / 24px larger-screen padding | Static; no hover treatment or pointer cursor |
| Compact monitoring panel | Same border, radius and shadow | Existing Overview 12px / 16px padding | Only its actual controls react to hover |
| Navigation tile | Same panel surface | Layout follows the directory contract | `bg-accent/60` hover, `bg-accent` pressed, visible focus ring |
| Inset / meter | 8px radius, muted fill, no shadow | Usually 8–12px | Hover only when it is an actual control |
| List | 12px outer radius, 1px border, no shadow | Rows own their padding | ListRow already defines hover/pressed/focus behavior |
| Dialog | 16px radius, popover surface, `shadow-md` | 16px / 24px | Shared close behavior, focus management and scrim |
| Control | 8px radius; border/fill belongs to the shared variant | 36px desktop height, at least 44px phone target | Shared hover, focus, disabled and invalid states |

Use the existing shadow definitions: xs = `0 1px 2px rgb(23 27 34 / 4%)`,
sm = `0 1px 3px rgb(23 27 34 / 6%)`, md =
`0 8px 24px rgb(23 27 34 / 12%)`. Borders remain the primary surface boundary
in dark mode. No hover lift, scale, animated shadow or nested elevated cards.
Color/opacity transitions take 150–200ms and honor reduced motion.

Whole-tile navigation uses a real link; an operation uses a button. A panel
containing settings and multiple actions is not one large click target. Keep
secondary actions outside a parent link/button. Keyboard focus uses a visible
2px ring in `ring`; use an inset treatment where overflow would clip it.
Hover never reveals the only path to an action. Selected, pressed, disabled,
pending and read-only are different states; disabled controls do not react to hover.

## Typography roles

Use Geist Variable with the current system sans fallback throughout. Reserve
monospace for machine-readable identifiers and literal output. All roles inherit
theme foreground unless stated otherwise; do not make ordinary values primary blue.

| Role | Size / line height | Weight | Color / other rules |
| --- | --- | --- | --- |
| Page / entity title | 24 / 32px | 600 | Foreground, tracking -0.015em |
| Section / panel title | 16 / 24px | 600 | Foreground |
| Compact monitoring title | 14 / 24px | 600 | Foreground; explicit monitoring variant |
| Directory item name | 14 / 20px | 500 | Launcher tile name below the mark (Access, Advanced); 13px on phones |
| Body / ordinary value | 14 / 20px | 400 | Foreground |
| Property label | 14 / 20px | 400 | Muted foreground |
| Form label | 14 / 20px | 500 | Foreground; use Label with a control association |
| Supporting description | 13 / 20px | 400 | Muted foreground |
| Compact metadata | 12 / 16px | 400 | Muted foreground; not the default for important answers |
| Technical value | 13 / 20px | 400 | Monospace, foreground, wrap long identifiers |
| Inline metric | 14 / 20px | 600 | Tabular numerals |
| Summary metric | 20 / 28px | 600 | Tabular numerals |
| Diagnostic result metric | 30 / 36px | 600 | Tabular numerals, tracking -0.015em |

Visual size does not choose HTML heading level. CardTitle renders Heading and
accepts its `as` and `size`; do not nest another heading inside it.
PropertyRow uses `dt`/`dd` under `dl`; form labels are not property labels.
Mobile input text remains 16px to avoid browser zoom.

The scale is a default for shared chrome and information display. Chart labels
may need domain-specific sizes; do not force SVG/map labels into body components.
Existing Access group headings retain their documented 24px directory treatment.

## Values, measurements and units

An answer should be easier to find than its label. Use foreground for values,
muted foreground for labels and semibold for measurements, selected answers or
primary state. Ordinary prose values do not all need semibold.

Use MetricValue emphasis by context, not by page name:

- Inline: Overview rates and latency, device traffic, interface rates and table
  measurements. Preserve the established dense Overview layout.
- Summary: a primary count such as DNS requests or interface/device totals.
- Result: the decisive speed, latency or jitter returned by an explicit test.
  Supporting results use summary or inline emphasis.

Keep value and unit on one line, aligned on their baseline, with a 4px visual
gap. An inline unit shares the 14px size; summary/result units use 14px regular
muted text. For example, result `23.4` uses 30px semibold and `ms` uses 14px
regular. Do not use monospace just because a value is numeric. Apply tabular
numerals to the entire comparable numeric run; right-align numeric table values.
Allow the surrounding row/group to wrap without splitting the number from its unit.

Keep existing API formatting and precision until separately reviewed. Display
`Mbps` for bit rates and `MB/s` for byte rates accurately; do not restyle one
into the other. Retain Download then Upload ordering and receive/transmit colors
when direction needs distinction. A fast or large value is not automatically
success-colored; warning/error requires a meaningful threshold or observed state.

Loading reserves the eventual value space with a skeleton. Unknown/unavailable
uses a dash with accessible context or explicit text; a failure has a nearby
explanation/retry. Verified zero is `0` with its unit. Disabled, empty, stale and
unavailable must not collapse into the same zero/dash state. Do not announce
every live numeric refresh through a live region.

Property rows align labels left and answers right on wide screens, have 36px
desktop / 44px phone minimum height, and grow for wrapping text. Use `min-width: 0`
on value containers. Long addresses wrap with `overflow-wrap: anywhere`; do not
silently truncate an important value or depend on a hover title to recover it.
On narrow layouts, reflow the answer below its label when needed. Editable
answers use a real focusable control with an accessible action name.

## Theme, state and accessibility

Reuse the light/dark semantic colors in `index.css` and the visual guide.
StatusIndicator uses neutral for pending/unknown/disabled, success for observed
healthy, warning for attention/pending consequences, and destructive for failure.
Always include a word or accessible icon label; a status dot alone is insufficient.
Badge identifies a compact category/attribute such as Fixed, not every value.
Alert explains an actionable condition; toast confirms a transient action and
does not replace persistent validation or apply failure feedback.

Check text contrast at least 4.5:1 for normal text and 3:1 for large text;
essential control boundaries and focus indicators need 3:1 against adjacent
colors. Muted is a semantic token, not permission to reduce opacity arbitrarily.
Check focus, wrapping, units, busy/error states and touch access in both themes.

## Implementation sequence and acceptance

The initial implementation covers radius tokens, headings/surfaces,
Tools/Overview/Gateway/DNS measurements, device/group properties, setting answers,
authentication headings and dialog depth. Ping without replies renders unavailable
latency. Phone input typography takes precedence over compact text utilities;
latency configuration actions use shared buttons and retain touch targets.

Reuse these owners before extracting more components. A general form-field wrapper
and a chart frame remain possible extractions when repeated behavior warrants
them. The control language — tones, status shapes, switch placement, button rank
and row glyphs — is specified in
[the control language contract](ui-control-language.md), and
`web/scripts/test-control-language.mjs` enforces the parts that text can check:
no page writes a status colour, no 10px status dot, every switch names its
subject, and "apply anyway" is never painted destructive. Keep data fetching, formatting and apply behavior local.

For implementation, review 375px, 768px and 1440px, both themes, keyboard-only
navigation, long IPv6/device names, missing data and live updates. Capture
representative before/after screenshots of Overview, a device, Gateway, a tool
result, a form/dialog and Access. Run UI lint/build and relevant behavior tests;
follow repository checks for shipped code changes. Mocked browser checks verify
presentation and interaction, not a live router's network behavior.
