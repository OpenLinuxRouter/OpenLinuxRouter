# Control language — states, switches, buttons, row actions and edit

Visual specimen: [ui-control-specimen.html](ui-control-specimen.html). Open it in
a browser; it restates the tokens by hand and is illustrative, not built.

This is the shared contract for the five control atoms every page has. It
follows the [visual guide](ui-style-guide.md), the
[semantic component contracts](ui-semantic-components.md) and the
[audit](ui-control-audit.md) that motivated it. Where this document and a page
disagree, this document wins; where it is silent, the visual guide applies.

The rules exist to answer four questions the same way on every screen: **what
state is this thing in, how do I change it, what else can I do to it, and where
does that lead.** A reader should learn the answer once, on the first page they
open.

## Principles

1. **One fact, one shape.** A state is drawn in exactly one of three shapes, and
   the shape says what kind of thing is being reported (see §2).
2. **Tone belongs to the word, not to the page.** "Running" is green wherever it
   appears; a page cannot decide otherwise.
3. **The dot carries tone, the word carries meaning.** Colour is never the only
   signal, and the word is never tinted to compensate for a weak dot.
4. **The glyph is the verb.** Chevron navigates, pencil edits, ellipsis offers a
   menu, rotating chevron expands. No glyph is reused for a second verb.
5. **One primary action per region.** A page header and a dialog footer each have
   at most one filled button.
6. **One control per row, one per card header.** A second action belongs on the
   page the row leads to, in the card body, or behind one menu (§6).
7. **Off is not a fault, and "I cannot see" is not a fault either.** The only red
   things on a page are things that are broken and that this page can fix.
8. **Never invent a control from a class string at the call site.** If a state,
   switch, button or row action needs a new look, it gets a variant.

## 1. The five tones

| Tone | Means | Dot (6px / 8px) | Word | Pill |
| --- | --- | --- | --- | --- |
| `ok` | observed healthy, working as asked | `bg-success` | `text-success-foreground` | `bg-success/10 text-success-foreground` |
| `warn` | working but needs a look: drift, degraded, blocked by a dependency, never connected | `bg-warning` | `text-warning-foreground` | `bg-warning/10 text-warning-foreground` |
| `fault` | broken or cannot work, and something here can fix it | `bg-destructive` | `text-destructive` | `bg-destructive/10 text-destructive` |
| `off` | deliberately switched off; nothing is expected | `bg-muted-foreground/40` filled | `text-muted-foreground` | `bg-muted text-muted-foreground` |
| `unknown` | we cannot see it: reading, unreadable, never checked | hollow ring: `border border-muted-foreground/60`, no fill | `text-muted-foreground` | `border border-border bg-transparent text-muted-foreground` |

Two decisions worth stating explicitly:

- **`off` and `unknown` differ by shape, not opacity.** Both are neutral, but a
  filled dot means "we know, and it is off" while a hollow ring means "we have no
  answer". At 6px an opacity difference is invisible; a ring is not.
- **A failed read is `unknown`, not `fault`,** because the module is not
  necessarily broken and the alert carries the error. The one exception is a
  page's own verdict sentence (§2.3): "Status unavailable" is the top-line answer,
  and an operator cannot act on a page that cannot read the box, so the verdict
  escalates `unknown` to `fault` — one red thing, at the top, once.

Retire every other neutral: `bg-muted-foreground/50`, `/45`, `/80`,
`bg-success/70`, `bg-success/12`, `bg-warning/15`, `bg-destructive/12`,
`text-muted-foreground/50`, `/60`.

## 2. Three status shapes

| Shape | Owns | Exactly one per… | Example |
| --- | --- | --- | --- |
| **StatusPill** | the state of the surface's subject | card or page header | `Running` beside *Connection settings* |
| **StatusInline** | the state of an item in a list, stack or table | row | dot + `Connected` in a device row |
| **StatusStrip** | the verdict for a whole module or page | page | *"Handing out addresses"* + detail + master switch |

### 2.1 StatusPill

24px tall, 6px radius, 12px medium text, no dot. It is the answer to "is the
thing this card is about working?", placed immediately after the card title.
Do not use it for tags, values or measurements (§2.4).

### 2.2 StatusInline

A 6px dot plus a 13px word, on one line, vertically centred with the surrounding
text. Two emphases:

- `answer` (default): the state is the row's payload — foreground text, medium
  weight. Used in `ListRow` trailing, table cells, stack cards.
- `dense`: the state repeats down a compact list and the list is about something
  else — muted word, tone only on the dot. Used only for the Overview module list.

The strip and page verdicts use the 8px dot; inline and dense use 6px. There is
no third size.

### 2.3 StatusStrip

The existing card, with three changes:

- `tone: Tone` replaces `dot: string`. Callers stop writing colour classes.
- `density: 'page' | 'compact'`. `page` is the standalone card
  (`p-4 sm:p-6`); `compact` is the block that DNS and DHCP currently hand-copy
  inside their landing card (`p-5`, extra summary rows as children).
- The master switch, drift row, details disclosure and children stay as they are;
  the switch is the only switch the strip draws, and the headline sentence names
  it.

### 2.4 Data is not status

A measurement (bytes, counts, rates, scores, addresses) is a value, never a pill.
The one judgement that stays a pill is a **grade** — `Excellent`, `Fair` — because
it is a verdict about a reading, not the reading. `Not counted` and `Nothing yet`
are `off`/`unknown` statuses, not metadata.

## 3. Words and their tone

Tone is fixed by the word. The dictionary lives in one file
(`web/src/lib/state.ts`) and every screen reads it.

| Tone | Words |
| --- | --- |
| `ok` | Running, Connected, OK, Working, On, Up to date, Delegated, Advertising, Configured, IPv6 enabled, Tunnel, Via exit, Default route, Valid for {days} days |
| `warn` | Needs attention, Not yet applied, Not up to date, Behind shared NAT, Renewal overdue, Not answering, Never connected, No cable, No address, Not configured, No network, No route, No default route, Unknown exit, Bypasses the exit, Partial, LAN offline |
| `fault` | Not running, Down, Expired, Not installed, Not set, Unreachable, Not found |
| `off` | Off, Disabled, Not needed yet, Blocking, No IPv6, No routes, Not managed, Nothing yet, IPv6 disabled, Not in use, Direct, Loopback, Blocked |
| `unknown` | Checking…, Reading…, Applying…, Waiting, Unknown, Not checked, Unavailable, Not counted, Status unknown, Asking…, Updating |

Domain words stay domain words: DHCP's "Devices disconnect" and DNS's are
different events and keep their own labels
(`features/plan/plan-preview.tsx`). Only the tone is shared. Adding a word means
adding a row here in the same change.

### 3.1 One derivation per subject

Three helpers, one per kind of subject, all returning `{ tone, word }`:

```ts
moduleState({ enabled, running, drift, blocked, reading, unreadable })
// reading → unknown "Checking…" · unreadable → unknown "Unavailable"
// !enabled → off "Off" · blocked/drift → warn → !running → fault "Not running"
// otherwise ok "Running"

linkState({ present, up, hasAddress })      // Not found · No cable · No address · Connected
peerState({ unknown, online, lastHandshake }) // Unknown · Connected · Never connected · <elapsed>
```

They replace `describeStatus()` in `routes/dhcp/index.tsx` and
`routes/dns/index.tsx`, `moduleState()` in `routes/overview.tsx`,
`describeState()` in
`features/link/interface-visual.tsx` and the seven-branch `dot={…}` expressions in
`routes/advanced/*`.

## 4. Switches

**One size:** 44 × 24px, 18px thumb, with a 44px hit area at every breakpoint.
Drop the unused `size` prop rather than shipping a variant nothing uses.

**Three legal placements:**

| Placement | Owner | Rule |
| --- | --- | --- |
| Setting with a consequence | `SwitchField` (`variant="panel" \| "inline"`) | visible label + one-line hint; the label states the consequence, not the mechanism |
| Master switch for the page's subject | `StatusStrip`, or a card header beside a `StatusPill` | the switch is labelled by the subject: `aria-labelledby` the visible card title, or a visible `Label` when the title does not name it. `sr-only` only under the strip, whose headline already names it |
| Per-row switch | list row | `sr-only` `Label` naming the row's object; never a bare switch with only `aria-label` if a visible word could fit |

Never a switch whose label appears nowhere on screen. The defect in today's
cards is not the position, it is that the accessible name ("WireGuard") differs
from the only visible words on the row (*Connection settings*, `Running`), so a
screen-reader user and a sighted user are told different things. Point
`aria-labelledby` at the title and the pill, and the two agree without adding a
redundant label.

**Busy is not disabled.** `busy` sets `aria-busy`, keeps the switch's contrast,
and pairs with a state word that says `Applying…` (`unknown`). `disabled` is
reserved for "the router will refuse this" — loopback adoption, missing binary,
no tunnel address.

## 5. Buttons

### 5.1 Rank

| Variant | Use | Count per region |
| --- | --- | --- |
| `primary` | the one action that commits: Save, Create, Add, Publish, Run | at most 1 |
| `secondary` | everything alongside it: Settings, Cancel-in-a-form, Add when the page's primary is elsewhere | any |
| `quiet` | low-emphasis actions in dense rows, dialog Cancel, Revert | any |
| `danger` | destroys or removes data: Remove, Delete, Forget, Stop publishing | any |
| `link` | navigation dressed as text | any |

The ranks are named for their role, not their paint: the old `outline` is
`secondary`, `ghost` is `quiet`, `destructive` is `danger`, the old filled
`secondary` is `subtle`, and the old default is `primary`. A paint name is now a
lint failure rather than a habit.

### 5.2 Size

| Size | Height | Text | Padding | Mobile |
| --- | --- | --- | --- | --- |
| `sm` | 32px | 13px | `px-3` | min 44px |
| `default` | 36px | 14px | `px-3.5` | min 44px |
| `lg` | 44px | 14px | `px-4` | 44px |
| `icon` | 36px | — | square | 44px |
| `icon-sm` | 32px | — | square | 44px |

`sm` must actually be smaller than `default` (today both are `h-9`). Remove
`icon-xs`; use the standard icon sizes for page actions. Overview detail pages
use normal navigation and have no floating fullscreen close controls.
Icons inside buttons are 16px, never `size-5`.

### 5.3 Placement and labels

- Page header: one primary, right-aligned, `PageHeading action`. **`Add` is
  primary when the page's purpose is to add that object** (DHCP ranges and
  reservations, DNS names and blocking, Gateway networks, WireGuard devices,
  port forwards, DDNS names, Caddy services). It is secondary only when the page
  already has a primary action.
- Card header: `CardAction`, secondary by default, or primary when the card is
  the page's only object.
- Dialog footer: destructive action first and left-aligned, then `spacer`, then
  `quiet` Cancel and the single `primary` commit. The commit verb is the object's
  verb — Save, Create, Publish — not "OK".
- Destructive confirmations use `danger` for the confirming button; the dialog
  title already says what is being removed.
- **"Apply anyway" and "Take it back anyway" are not destructive.** They are the
  primary action of a warning dialog; the warning is the `Alert` above them.
  Today five editors paint them red, which teaches that red means "continue".

### 5.4 Icon-only buttons

One `RowAction` component (§6.2) for actions inside rows and toolbars. Anything
icon-only carries `aria-label` naming the object and the verb ("Edit Li's
iPhone", "View wan1", "Change iCloud icon") plus a `title` with the same string.
No bare `+`, `↑`, `↓` text glyphs as icons.

## 6. Row actions: one control per row or header

### 6.1 The rule

**A row or a card header shows at most one control.** Not a chevron beside a
pencil, not a button beside a switch. Everything else belongs on the page the
row leads to, in the card body, or behind the one menu.

| The row's job | Whole row | Its one control | Example |
| --- | --- | --- | --- |
| Leads to a page | link | `chevron-right` 16px | WireGuard device |
| Opens an editor and has no page | button | `pencil` 16px in a `RowAction` | port forward, DHCP range, DNS name |
| Shows a setting | link, or static when the control is the point | that control only, with no chevron | interface row's adoption switch on its own page |
| Has two or more secondary actions | link or button | `more-horizontal` → `RowMenu` | topology group |
| Read-only record | static | none | gateway traffic sample |
| Expands in place | `<details>` | `chevron-right` 14px, rotates 90° | technical details |

Corollaries, which are what the rule actually costs:

- **Editing, removing and configuring live on the detail.** A row leads to the
  object's page and the page owns its operations: a WireGuard device's rename,
  routes and removal are on the device page, not on the list. Where the object
  has no page, the editor *is* the dialog and the pencil opens it — so a row that
  opens a dialog still shows a pencil, and one that navigates shows a chevron.
- **A control displaces the chevron.** When a row shows a switch, the row itself
  is still the link and no chevron is drawn. Two visible controls in one trailing
  slot is what made the page read as a toolbar per row.
- **A card header shows one control too.** The switch when the card is about
  turning something on; otherwise the one action that changes it. A settings
  entry that would sit next to the switch becomes a line in the card body
  (WireGuard's connection settings, SOCKS5's link).
- **A menu is one control.** Several operations may hide behind one ellipsis;
  they may not line up as several icons.

The one place a row expands in place rather than navigating (`Disclosure`) keeps
its rotating 14px chevron, and the two hand-rolled disclosures in
`features/network-quality/network-quality.tsx` should use it.

### 6.2 RowAction

36 × 36px (44px on phones), 8px radius, 16px icon, `text-muted-foreground`,
`hover:bg-accent/60`, visible focus ring, positioned after the trailing value
with 12px to the card edge. Never one row with three adjacent icon buttons
(Overview's site editor has three today): one visible action plus a menu.

### 6.3 RowMenu

`DropdownMenu` under a `more-horizontal` `RowAction`: minimum 192px wide, 36px
items with 16px icons, separators between action groups, destructive item last,
`align="end"`, Escape closes, focus returns to the trigger. The trigger's
accessible name is "{object} options".

## 7. Edit

- **One entry per object, and it lives with the object.** The list navigates; the
  page (or the dialog behind its pencil) edits. Nothing is editable from two
  places at once.
- **The control is the value.** Editable values in a property row are the
  control (select, segmented control, switch, in-place input) — not a pencil
  beside a static value.
- **One edit entry per entity.** Entity → its page → its fields; or, with no
  page, the row's pencil. Both patterns may not appear for the same kind of
  object.
- **Inline title editing stays a documented exception** for device names only
  (`device-inline-editor.tsx`), because the name is the object and the list is
  where it is read.
- **Tags that open a dialog are buttons.** The group tag becomes `TagButton`
  (24px, hover, focus, `aria-haspopup="dialog"`), not a `Badge` wrapped in a `Link`
  whose destination is a dialog.
- A dialog's own destructive action lives in its footer, styled `danger`, and
  confirms first if it is irreversible.

## 8. Tokens and files

| File | Owns |
| --- | --- |
| `web/src/components/ui/status.tsx` | `Tone`, `StatusDot`, `StatusInline`, `StatusPill`; tone → class maps |
| `web/src/lib/state.ts` | `WORD_TONE`, `moduleState`, `linkState`, `peerState` |
| `web/src/components/ui/row-action.tsx` | `RowAction`, `RowMenu`, `RowChevron` |
| `web/src/components/ui/button.tsx` | ranks renamed, sizes corrected |
| `web/src/components/ui/switch.tsx` | `busy`, no dead `size` |
| `web/src/components/layout/status-strip.tsx` | `tone`, `density`, compact copy adopted by DNS/DHCP |

Only the tone maps know Tailwind colour classes. Charts, maps and the alert
component keep their own scales, because hue means something else there.

### Portability across apps

These five primitives depend only on the theme tokens and `cn()`: no routing, no
queries, no i18n beyond label props. `WORD_TONE` is the only product-specific
file. To reuse the language in another app, copy `status.tsx`, `row-action.tsx`
and the button variants, restate `WORD_TONE` in that product's words, and point
the tokens at its palette. Keeping them in one folder is what makes that a copy
rather than a refactor.

## 9. Migration

Each phase ships on its own and is verifiable by screenshot.

| Phase | Change | Files |
| --- | --- | --- |
| **P0** | Add `status.tsx`, `state.ts`, `row-action.tsx`; `StatusStrip` accepts `tone`/`density`; implementations emit today's classes so nothing moves | 4 shared files |
| **P1** | Words and tones: adopt helpers, one dot size pair, hollow `unknown`, retint `Running`/`Unavailable`/`Off`, compact strip in DNS and DHCP, grade pill via `StatusPill`, tags out of `Badge` | `overview`, `gateway/*`, `link/*`, `dial/uplink-card`, `remote/*`, `dhcp/index`, `dns/index`, `advanced/{ddns,caddy,iptv,forwards}`, `nat/forward-list`, `gateway/exit-list`, `network-quality`, `dns/activity`, `group`, `devices` |
| **P2** | Buttons: ranks renamed with aliases, `sm` 32px, one primary rule, Apply-anyway → primary (6), Cancel → quiet (5), Remove → danger (2), Add hierarchy (10) | `components/ui/button.tsx` + every route listed in the audit §5 |
| **P3** | Rows: pencil grammar for the 9 dialog rows, chevron geometry and opacity, RowMenu adoption, Overview site actions, Caddy external link | the 9 rows + `list.tsx` + `topology/nodes.tsx` |
| **P4** | Switches: visible labels on WireGuard/SOCKS5, DNS query-log into `SwitchField`, pool-dialog label, `busy` vs `disabled` | `remote.tsx`, `socks-card.tsx`, `dns/index.tsx`, `dhcp/pool-dialog.tsx`, `switch.tsx` |
| **P5** | Docs and guard: visual guide "Controls, lists and dialogs", semantic inventory (StatusIndicator shipped → `StatusInline`/`StatusPill`; add `RowAction`), page-review record, screenshots | `docs/*`, `web/scripts/test-control-language.mjs` |

### Guard test

`web/scripts/test-control-language.mjs` (node, like
`test-interface-traffic.mjs`) fails the build when:

- a tone colour class appears outside the status/alert/badge/chart allowlist;
- a `size-2.5` or `size-1.5` status dot appears;
- `<Switch` has neither `aria-label` nor an `id` with a `Label htmlFor`;
- a `Button` labelled `Apply…`/`Take it back…` uses `variant="destructive"`;
- `variant="success|warning|destructive"` appears on a `Badge` outside
  `components/`.

## 10. Acceptance

- One tone per word, asserted from `WORD_TONE`; no page chooses a colour.
- Dots are 6px or 8px; `off` is filled, `unknown` is a ring; `fault` appears once
  per page, in the verdict.
- Every status has a word; every ring/dot has an accessible label.
- One primary button per page header and per dialog footer.
- Every interactive row's glyph matches its behaviour; no chevron opens a dialog.
- Every switch has a visible label at card/page level and 44px targets on phones.
- `npm run lint`, `npm run build`, `npm test`, `make check`; screenshots at
  375 / 768 / 1440 in both themes for Overview, WireGuard, a Gateway stack,
  DNS, one dialog and one tool result.

## 11. Choices worth arguing about

These are deliberate, reversible, and would change what a phase looks like:

| Choice | What it costs | The alternative |
| --- | --- | --- |
| `unknown` is a hollow ring, not a fourth opacity | one visual idea to learn | keep neutral grey and accept that "off" and "cannot see" look identical |
| A failed read is `unknown` per module, `fault` at the verdict | two rules instead of one | red every module row, which is today's Overview and makes one bad read look like three outages |
| Row states become dot + word instead of a pill | four screens change (peer list, port forwards, exits, DDNS) | keep pills in rows and accept two status shapes side by side |
| `sm` becomes 32px | 66 call sites shrink by 4px | leave `sm` at 36px and delete the name, so "small" keeps meaning "not small" |
| Variants are renamed `primary/secondary/quiet/danger` | 161 call sites, staged with aliases | keep paint names (`outline`, `ghost`) that do not say the rank |
| "Apply anyway" becomes primary | six dialogs look less alarming | keep red, and teach that red means "continue" |

## 12. Non-goals

- Not a design-system package, and not a component per text size.
- Domain wording is untouched; the plan-impact labels stay per module.
- Chart, map, Sankey, activity-matrix and radar colours stay domain scales.
- The Overview hero tile, the status-strip sentence and the map's peer dot are
  deliberate one-offs and keep their documented behaviour.
- No optimistic writes or apply-pipeline changes: this is presentation only.
