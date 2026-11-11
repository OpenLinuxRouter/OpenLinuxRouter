# Device icons

One picture per category, and optionally one per vendor's take on a category.
These are shipped inside the `olrd` binary (`internal/webui/assets`), so their
size is the binary's size.

## Three tiers, one look

**Tier 1 — category icon.** One per `DeviceCategory`. Always present, and the
common case: a DHCP fingerprint or an OUI narrows a client to a *class* far more
often than to a model. Generated from text, because a generic laptop has no
ground truth to be wrong about.

"Brandless" here means no logo, no model, no brand-signature silhouette or
colourway — **not colourless**. A category icon looks like a real device in
real materials, just drawn cleanly. Tier 1 has to look finished on its own, because tier 2 is
optional and licence-gated: a deployment with nothing but category icons must
look intentional rather than degraded. The tiers are distinguished in the UI by
the *label* — "Laptop" where only the class is known, "MacBook Air" where the
model is — and by nothing else.

**Tier 1.5 — vendor icon.** One per `<vendor>/<category>`, e.g. `apple/laptop`,
overriding the plain category icon. Named by the closed `VendorKey` vocabulary
in `internal/clients/vendor.go`, and registered under exactly that path in
`IMAGES`, where a template literal type checks both halves at compile time.

This is the one tier that **relaxes a shared rule**: brand-signature silhouette
and colourway are allowed here, because they are the entire point. Everything
else still holds, and two bans are absolute — **no logos and no text**. An Apple
laptop is a thin aluminium wedge; it is not a laptop with an apple on the lid.
Generated from text like tier 1, and legitimately so: "an Apple laptop" is a
class with a recognisable house style, not a product with a serial number.

**Vendor-only icons do not exist, and that is a decision.** A picture for "an
Apple something" has to choose between a phone, a watch, a laptop and a TV box,
which is inventing exactly the information `detect.go` refuses to invent. A
device with a vendor and no category gets its vendor's **initials** in the quiet
tile instead — see `vendorInitials` in `features/devices/icons.ts`. That also
covers all thirty thousand vendors in the IEEE registry rather than the few
dozen anyone will ever draw, which no set of assets can.

**Variants — one shape of a kind.** Some kinds of device come in shapes too
different to share a picture: an eight-bay NAS is not the two-bay box, and a
Mac mini is not the iMac that `apple/desktop` draws. A variant is keyed under
what it is a shape *of* — `server/tower`, `apple/desktop/mini`,
`synology/nas/8bay` — and the list is closed, in `variants` in
`internal/clients/icon.go` and hand-mirrored as `VariantKey` in
`features/devices/icons.ts`. The brandless pair matter most at home, because
they are what a home runs: `server/tower` is the PC under a desk that is really
a server, and `desktop/mini` is the small box on a shelf that is really a
computer — neither is the rack unit or the monitor-and-tower the plain category
draws. Detection never picks one; only an operator does, from the picker, where
brandless variants sit beside their category's tile and vendor ones among the
brands. Drawn like the tier they belong to — a brandless variant is tier 1,
Apple's is tier 1.5 — and changed from its base in exactly one respect.

**The NAS family is Synology's.** A NAS box comes in as many shapes as it has
bays, so the family is a variant set — and it hangs off the maker, not the
category, because the box everyone pictures *is* the Synology DS tower that
this set was drawn from. Filing it under `nas/` claimed a generic picture for a
shape that is really one maker's, and buried the maker in *Other brands* with a
single tile. So:

- **The two-bay box is the vendor picture**, `synology/nas`. It is what a
  detected Synology NAS wears before anyone says how many bays it has, which is
  exactly the count nobody can detect.
- **The other counts are variants**, `synology/nas/<n>bay`, one prompt with the
  bay count swapped so the family reads as one product line. The closed list is
  one to twelve: **1, 3, 4, 5, 6, 8, 12**. The vocabulary is allowed to run
  ahead of the artwork — a count with no master is a valid value the picker
  never offers — which is how 1, 3 and 12 are ready the day their pictures land.
- **The old `nas/<n>bay` keys still validate and still draw**, from the same
  pictures, because a config written before the move must load unchanged. They
  are not offered anywhere: the vocabulary is history, not a menu. See
  `MOVED_ICONS` in `features/devices/icons.ts`.

With that, the Common grid is left with the categories and the two brandless
shapes, which is what it is for.

Which counts have a master — all eight, drawn as one batch so they share a
material, a camera and a caddy:

| Bays | File |
|---|---|
| 1 | `synology-nas-1bay.webp` |
| 2 | `synology-nas.webp` — also the vendor picture |
| 3 | `synology-nas-3bay.webp` |
| 4 | `synology-nas-4bay.webp` |
| 5 | `synology-nas-5bay.webp` |
| 6 | `synology-nas-6bay.webp` |
| 8 | `synology-nas-8bay.webp` |
| 12 | `synology-nas-12bay.webp` |

**One batch, then one tone.** Eight calls to a text-to-image model do not agree
on what "matte charcoal" means: the first pass over these came back anywhere
from a mean luminance of 47 to 78, and eight tiles in a row make that
difference the first thing anyone sees. Two fixes, and both are needed:

1. **Draw them in one sitting**, from one prompt with only the count changed.
   That keeps the drawing language — corner radius, caddy handles, button — the
   same, and it narrowed the tone spread from 1.7× to 1.15×.
2. **Match what is left** with a single linear gain per icon, so the mean
   luminance of the drawn pixels agrees to within a point. A gain is not a
   redraw: it is the same adjustment the optimiser already makes to framing,
   applied to brightness, and it is why the eight read as one product line
   rather than eight renders of one idea. The measured target is the batch's
   own median, never a fixed number — the family is matched to itself, not to a
   colour the set does not otherwise use.

This is the one family where the *whole* subject is a number, so it is exactly
the case text-to-image is safe for, and adding a count is one word. The subject
sentence, on top of the preamble in "Adding one":

> Subject: **a desktop NAS enclosure in matte charcoal grey, mid-dark neutral
> grey, front view, one row of \<n\> vertical front-loading drive caddies side
> by side with even spacing, caddy width constant, the enclosure wider than it
> is tall and wider still as the count grows, a small round power button low on
> the right of the front panel**. Very minimal: a few large simple forms, no
> fine textures, no small parts, no vents, no screws, no perforations, no LEDs,
> no labels, no drives sticking out.

Two counts want their wording bent, and both were: **one bay** is *a single
vertical front-loading drive caddy filling most of the front* of a tall narrow
box — "one row" reads as a row of one and comes back too wide — and **twelve**
wants *very wide, low* and *narrow caddies*, or the bays come out fat and the
box square.

Adding a count is the usual three steps: optimise the master into this
directory, add its line to `IMAGES` and its name to `variants` in
`internal/clients/icon.go` if it is a new count, and nothing else moves.

**How the picker arranges them.** The picker is a search box over two
catalogues, and below it two blocks in this order:

1. **Brands**, one labelled section per maker — Apple, Samsung, Huawei.
2. **Common**, one grid mixing every picture that is nobody's product: the
   categories and their variants, phones beside washing machines. Its order is
   `CATEGORY_ORDER`, so the list still reads personal-devices-first and the
   grey box last, but it draws no headings. A grid is scanned; a table of
   contents is not, and nobody wants to decide whether the box in the hall is
   filed under Home or Computing before they can pick it.

Brands lead because a brand is a name the operator already has. The grouping is
derived from the artwork in `IMAGES`, not listed beside it, in `brandGroups()`
in `features/devices/brand-groups.ts`:

- A maker with **two or more pictures** gets its own section, headed with the
  maker's name. The name comes from `BRAND_NAMES`, never from the slug: a slug
  is a filename, and deriving a caption from one produced "Tp-link".
- A maker with **one picture** does not earn a heading. A dozen headings over a
  dozen single tiles is longer to scan than the flat grid it replaced, so those
  tiles — which already name their maker ("Brother printer") — collect under a
  final *Other brands* section.
- Sections are ordered by **how many pictures they hold**, ties in artwork
  order, so that adding a maker's second picture promotes them without anyone
  editing a list.

Adding a picture is therefore still one file and one line in `IMAGES`. The tile
lands under its maker if they have a section, in *Other brands* if they do not,
and a maker leaves that bucket the moment they hold two.

**The search answers from both catalogues.** One box finds a maker, a model and
a kind of device alike: theSVG's six thousand marks are matched on the daemon
(`internal/brandlogo`), and the pictures in the bundle are matched in the
browser against their labels, their maker, their category and their **tags** —
`ICON_TAGS` in `features/devices/icons.ts`, which is where "storage" finds the
NAS and "socket" finds the smart plug. A new picture needs a line in `ICON_TAGS`
only when nobody would type its label; the vocabulary is keyed by `IconKey`, so
a tag cannot name a picture that does not exist. While a query is active the
two blocks are replaced by the results, bundled pictures first, theSVG's marks
under a heading of their own.

**Tier 2 — model image.** One per specific product, e.g. `synology/ds224plus`,
overriding its vendor and category icons. **Never text-to-image**: a prompt for
"Synology DS224+" yields a convincing two-bay NAS that is not a DS224+, and an
inventory whose pictures are subtly wrong is worse than one that admits it only
knows the category. Tier 2 is made by *restyling a real photograph* — the photo
fixes the silhouette and proportions, the edit normalises view, light and
finish. True product colours are kept.

The line between 1.5 and 2 is where the prompt stops being able to be right. A
house style is a real thing a text-to-image model knows; a part number is not.

The iKOOLCORE R1, R2 and R2 Max are photo-restyled model artwork, exposed as
explicit `desktop/ikoolcorer1`, `desktop/ikoolcorer2` and
`desktop/ikoolcorer2max` picker variants beside Mini PC. Their manufacturer
references and exact edit prompts are in [ikoolcore-sources.json](ikoolcore-sources.json).
R1 retains its graphite enclosure and paired copper-edged openings; R2 its
silver compact enclosure; R2 Max its wide, low silver enclosure. All use the
same front view, simplified materials and transparent framing as the set.
They are manual choices: an Intel NIC does not identify the mini PC's maker.
`Device.model` still does not automatically select artwork.

### Licensing gates tier 2

Restyling a product photo creates a derivative of that photo. For an asset
embedded in `olrd` the source must be a manufacturer press-kit image used within
its terms, a permissively licensed image, or one we took. **Image-search results
are not a safe source.** Trademark use to *identify* a product in an inventory is
generally defensible; the photograph's copyright is the real exposure. Tier 1
has no such problem, which is another reason the product stays fully usable on
tier 1 alone.

## Resolution order

First match wins — `internal/clients/list.go`, and `deviceIcon()` in `icons.ts`:

1. **Model** — operator-set, or detected and then confirmed. *Not wired in yet.*
2. **Vendor + category** — `apple/laptop`
3. **Vendor initials** — only when the category is unknown, so there is nothing
   better to say. Not an asset; drawn from the vendor's name in the UI.
4. **Category** — operator-set, else detected
5. **`unknown`**

Two axes, each falling back on its own: a device whose vendor has no artwork
still gets its category's picture, and a category with no artwork still gets a
line glyph rather than a stand-in photograph.

Step 3 sits above step 4 only because there is no category to use. A picture of
the right *kind of thing* beats a mark of the right *brand* — someone scanning
the list is looking for their printer, not for Brother.

An operator override always beats detection. The icon is *identity*, not
presence (design.md §4.4), so it is stored config: a picture someone corrected
must not be silently changed back by the next fingerprint update.

## Shared rules

The look is the device pictures in a phone's settings app: the object seen
square-on, simplified, evenly lit, with its screen on. It replaced a matte
three-quarter product render in September 2026, for two reasons that only show
up in a real list:

- **At 32 px a dark screen is the whole icon.** A phone, a tablet and a laptop
  with their screens off are three black slabs; with the screens on, the
  outline — a notch, a Dynamic Island, a keyboard deck — is what you see.
- **Many vendors will sit side by side.** A front view is the one angle every
  vendor's product can be drawn at, and one shared wallpaper on every screen
  keeps a list of six brands calm. Each vendor gets its shape and its materials;
  nobody gets their own colours on the screen.

| | |
|---|---|
| **View** | Straight-on front, symmetrical, upright, camera very slightly above |
| **Light** | Soft and even, crisp edges; clean, simplified, semi-realistic |
| **Screen** | Always on, always the same wallpaper: a soft, blurred diagonal gradient from pale sky blue (top left) to periwinkle indigo (bottom right). No shapes, no UI |
| **Shadow** | None baked in — the UI supplies `drop-shadow`, so assets stay reusable on any background |
| **Framing** | Done by the optimiser, not the prompt: trimmed to the object, longest side ≤ 88% and area ≤ 70% of the canvas, centred |
| **Master** | 1024×1024 PNG. A near-white background is expected and removed by the optimiser |
| **Shipped** | 256×256 WebP, quality 0.86, transparent |
| **Forbidden** | Logos, text, stickers, brand marks; ground planes; status LEDs |

Tier 1 palette: neutral device materials — graphite, silver or matte white — and
nothing that belongs to one vendor.

Tier 1.5 is the one exception, and only to the *silhouette* rule: a vendor icon
may carry that vendor's proportions, materials and colourway, because
recognising them is what it is for. The screen is not part of that: the
wallpaper is the house's, never the vendor's. "No logos, no text" binds hardest
of all — the shape may say Apple, the lid may not.

**What the picture cannot do.** Seen from the front, a laptop's brand lives in
details that do not survive 32 px: a MacBook and a MateBook are the same grey
rectangle there, and only a ThinkPad's red dot still reads. There is nowhere to
put a logo on a front view either, and we would not ship one. Telling two
laptops apart is the row's job — its title and vendor — not the icon's.

## Operating-system marks

A device can wear an OS's logo instead of a picture — `os/debian`, stored as
the device's `icon` (see `internal/clients/icon.go`). This is the one place
logos are allowed, and deliberately: a picture of "a computer running Debian"
is a generic box, and the mark is the only thing that says Debian.

The picker no longer offers them. What a machine *runs* is a third axis beside
what it is and who made it, and a picker of two blocks — brands, then one
common grid — could not hold three without one of them becoming the block
nobody scrolls to. Stored `os/…` values still draw, `internal/devices` still
validates them, and putting a chooser back is a section, not a rebuild.

The rule against logos is about *device pictures*, where a brand mark would be
decoration; here the mark *is* the information.

They live in `../os-marks/`, one SVG each, in full colour from
[thesvg.org](https://thesvg.org) (MIT; `public/icons/<slug>/default.svg` in
GLINCKER/thesvg). Not generated, and never redrawn. They sit in the same
quiet tile as the glyphs, because a flat logo floating bare beside the
photographs reads as a sticker.

Two substitutions, both because the real mark fails at 32 px:

- **macOS and iOS share Apple's mark**; their own marks are wordmarks. Apple's
  is single-colour, so it ships twice — `apple-light.svg` (black) and
  `apple-dark.svg` (white) — and the theme picks.
- **HarmonyOS wears Huawei's flower**; its own mark is the black wordmark
  "HMOS".

Adding one is an SVG in `../os-marks/`, a line in `OS_MARKS` in
`features/devices/icons.ts`, and the same key in `operatingSystems` in
`internal/clients/icon.go`, which is what the stored value is validated
against.

## Protocol marks

The other place a logo is allowed, and the narrower one: WireGuard's knot and
Shadowsocks' paper plane, worn by the cards on the access pages that say how
somebody reached this router. Same argument as the OS marks — "a picture of
WireGuard" is a grey box — and the same treatment: flat colour artwork in the
quiet tile, never floating bare. The network map's dialled-in nodes used to wear
the knot too; they now draw the device's own picture, because a peer on that map
is a device to the person reading it (docs/remote-access.md §3.4).

They live in `../way-marks/`, one SVG each. WireGuard's is from
[thesvg.org](https://thesvg.org) (MIT) like the OS marks; Shadowsocks' is the
project's own paper plane, transcribed from the vector in its Android client.
Both carry the red the two brands share, which is why they sit together on one
page without either looking borrowed.

**Not part of the device vocabulary, and that is the difference from the OS
marks.** An OS mark is *identity* — it says this machine runs Debian — so it is
stored on the device and validated by `Icon`. Which way a connection arrived is
read from the status of whatever is running, is stored nowhere, and is therefore
just a lookup the UI does while rendering: `WAY_MARKS` in
`features/remote/way-marks.ts`, reached through `WayMark`. Adding one is an SVG
and a line in that map. Nothing in Go changes, and no config can name it.

The fallback is the caller's own line glyph rather than a stand-in picture, for
the reason every other gap in this set falls back to a glyph: SOCKS5 has no mark,
shares no artwork with Shadowsocks, and is better served by a plain icon than by
someone else's logo.

## Adding one

1. **Generate a master** at 1024×1024 with this preamble, changing only the
   subject. Keeping the rest verbatim is what holds the set coherent:

   > Clean device thumbnail illustration, like the device pictures in a phone's
   > settings app. Shown straight-on from the front, perfectly symmetrical,
   > upright, camera very slightly above. Clean simplified semi-realistic
   > rendering, soft even lighting, crisp edges, designed to stay legible at 32
   > pixels. Single object centred, filling about 80% of the frame, plain pure
   > white background, no shadow, no ground plane. The screen shows exactly this
   > wallpaper: a soft, blurred, low-contrast diagonal gradient from pale sky
   > blue at the top-left to soft periwinkle indigo at the bottom-right, no
   > shapes, no waves. No logos, no text, no UI, no brand marks. Subject:
   > **\<subject\>**

   The subject names the device and its materials. **Describe as little as
   will identify it**, and say so: end the description with *"Very minimal: a
   few large simple forms, no fine textures, no small parts."*, naming the
   details this device tempts the generator into — *no vents, no screws, no
   perforations, no cables, no mist*. Left to itself the generator fills a
   server with twelve drive bays and a purifier with a mesh of holes; the set's
   look is the NAS (two plain bays) and the speaker (one soft cylinder), and
   anything busier stands out at 32 px as a different set. A **tier 1**
   subject then ends with *"Generic brandless industrial design in neutral
   materials."*; a **tier
   1.5** subject describes the vendor's product instead — *"an Apple iPad Pro in
   portrait orientation, silver aluminium edge, uniform thin black bezels…"*.

   Two adjustments the generator has needed, and nothing else may change:

   - **No screen** (router, speaker, printer…): drop the *"The screen shows…"*
     sentence and end the subject with *"This device has no screen."* Left in,
     the generator adds a screen to things that have none.
   - **A large screen** (TV, monitor): end the subject with *"The gradient is
     only inside the screen; everything around it is plain pure white."* Without
     it, the first TV came back with the wallpaper as its background.
     A TV is drawn **wall-mounted, with no stand** — a flat panel with thin
     even bezels, the way a Sony Bravia hangs. The first set had a pedestal
     stand, which looked dated: say *"no stand, no feet, nothing below
     the screen"* or the generator adds one.

   The background is white rather than transparent because the generator we
   use rejects the transparency option. The optimiser removes it; see step 2.

2. **Optimise** it into this directory, named exactly after the category, or
   `<vendor>-<category>` for a vendor icon, with `-<variant>` appended for a
   variant (`synology-nas-4bay`, `apple-desktop-mini`):

   ```
   node scripts/optimize-icons.mjs <dir-of-masters>
   ```

   Chromium does the cut-out, framing, resize and encode; no image library is
   needed. The cut-out floods in from the border, so a white router keeps its
   white body — but check the result on a dark background anyway, which is
   where a leak shows. Masters are not committed — they are ~1 MB each and this
   file is how they are reproduced.

3. **Register it** — one line in `IMAGES` in `../../features/devices/icons.ts`.
   A vendor icon is registered under its slash form, `'apple/laptop': appleLaptop`,
   which is what the `IconKey` type checks; the file itself cannot contain a
   slash, hence the hyphen on disk. A
   variant also needs its name in `variants` in `internal/clients/icon.go`
   and in `VariantKey`, or the stored value is rejected.

Nothing here is required. A category with no image falls back to its line glyph
while still showing its own correct label; a vendor with no image falls back to
the plain category icon; a device with neither falls back to its vendor's
initials. Every gap has a deliberate answer, which is why the set can be filled
in one file at a time and has been.
