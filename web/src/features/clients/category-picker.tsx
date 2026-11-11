import { t, useLocale } from '@/lib/i18n'
import { useState } from 'react'

import { Input } from '@/components/ui/input'
import { brandLogoSlug, useBrandLogoSearch } from '@/features/brand-logos/brand-logos'
import { DeviceIcon } from '@/features/clients/client-icon'
import { BRAND_GROUPS, COMMON_TILES, type IconChoiceTile, searchBundledIcons } from '@/features/clients/icons'
import type { ClientCategory } from '@/lib/config-types'
import { cn } from '@/lib/utils'

/** What the picker chooses: a category, and optionally a picture that overrides its own. */
export interface IconChoice {
  category: ClientCategory
  icon: string
}

/**
 * Picks what a device looks like, by its picture.
 *
 * A drop-down would be smaller, but the thing being chosen *is* an image, and
 * choosing "nas" from a list of twenty-odd words to find out what it looks like
 * is a guess-and-check loop. Showing the pictures makes the choice the same
 * shape as the result.
 *
 * Two blocks, in this order, and the order is the whole arrangement:
 *
 *   1. **Brands**, one section per maker, for the pictures that are somebody's
 *      product — the iPhone, the Galaxy, the ThinkPad. Someone looking for the
 *      phone they own is looking for Apple, not for the thirty-first tile.
 *      Which makers get a heading is derived from the artwork rather than
 *      decided here; see BRAND_GROUPS.
 *   2. **Common**, one grid of everything that is nobody's product: the
 *      category pictures and their variants, mixed. Phones, NAS boxes, a
 *      washing machine, a four-bay NAS, a mini PC — the shapes you scan when
 *      you do not yet know the word for the thing.
 *
 * Brands lead because a brand is a name you already have; a kind of device is
 * what remains when you do not. That is also why this block is one grid and not
 * the six themed groups it used to be: a grid is scanned, and a table of
 * contents is not, and nobody decides whether the box in the hall is filed
 * under Home or Computing before they can pick it.
 *
 * Above both, one search box that answers from both catalogues — the pictures
 * in the bundle, matched on their labels and tags, and theSVG's six thousand
 * marks, matched on the daemon. Typing is the fast path for anyone who knows
 * the word, and it is the only path that can reach a picture filed under a
 * heading they did not think of.
 *
 * What each kind of tile stores, which is not the same for all of them:
 *
 *   - **A category** sets the category and clears any chosen icon, so the
 *     picture goes back to following category and vendor.
 *   - **A variant** — a 4-bay NAS — sets both too, and sits beside its
 *     category's own tile, where someone looking for their NAS will look.
 *   - **A brand's picture** — iPhone, MacBook — sets both: an iPhone *is* a
 *     phone, and the label should say so. This is the only road to the Apple
 *     pictures for a device whose randomised MAC hides who made it, which is
 *     most Apple devices.
 *   - **A theSVG mark** sets only the icon and leaves the category alone. A
 *     Proxmox box is still a server; the mark is how it is drawn, not what it
 *     is.
 *
 * Operating-system marks are no longer offered here, though stored `os/…`
 * values still draw (see `chosenIcon`). They are a third axis — what a machine
 * runs rather than what it is or who made it — and a picker of two blocks could
 * not hold three without one of them becoming the one nobody scrolls to.
 *
 * Categories without artwork are shown too, wearing a line glyph instead (see
 * DeviceIcon). Hiding them would be worse: the category is real and the label is
 * correct, and the operator's answer is worth storing now so that it is already
 * right on the day the picture lands.
 */
export function CategoryPicker({
  value,
  onChange,
}: {
  /** The stored override, with '' for each half nothing is set for. */
  value: IconChoice
  onChange: (next: IconChoice) => void
}) {
  useLocale()
  const [search, setSearch] = useState('')
  const query = search.trim()
  const bundled = searchBundledIcons(query)
  const { slugs, loading } = useBrandLogoSearch(search)
  const chosenSlug = brandLogoSlug(value.icon)

  // What a tile stores, worked out once: a category clears the icon, everything
  // else sets both. See the order of the four cases above.
  const choose = (tile: IconChoiceTile) => onChange({ category: tile.category, icon: tile.icon })
  const selected = (tile: IconChoiceTile) => value.icon === tile.icon && value.category === tile.category

  return (
    <div className="space-y-3">
      {/* The search box owns the top of the picker and the list below it
          scrolls, rather than the two sharing one scroller. A dialog that
          scrolls as a whole would carry the search box off the top of the
          screen exactly when the list is long enough to want it — which is
          always, with every maker's pictures in front of the common ones. */}
      <Input
        aria-label={t('Search icons')}
        placeholder={t('Search icons (e.g. nas, proxmox)')}
        value={search}
        onChange={(event) => setSearch(event.target.value)}
      />
      <div className="max-h-[min(30rem,60svh)] overflow-y-auto overscroll-contain">
        <div className="space-y-4">
          {query ? (
            <>
              {bundled.length > 0 && (
                <Section label={t('Built-in icons')}>
                  {bundled.map((tile) => (
                    <Tile key={tile.icon || tile.category} label={tile.label} selected={selected(tile)} onClick={() => choose(tile)}>
                      <DeviceIcon icon={tile.icon || undefined} category={tile.category} size="md" />
                    </Tile>
                  ))}
                </Section>
              )}
              {slugs.length > 0 && (
                <Section label="theSVG">
                  {slugs.map((slug) => (
                    <Tile
                      key={slug}
                      label={slug}
                      selected={value.icon === `thesvg:${slug}`}
                      onClick={() => onChange({ category: value.category, icon: `thesvg:${slug}` })}
                    >
                      <DeviceIcon icon={`thesvg:${slug}`} category={value.category} size="md" />
                    </Tile>
                  ))}
                </Section>
              )}
              {!loading && bundled.length === 0 && slugs.length === 0 && (
                <p role="status" className="text-sm text-muted-foreground">{t('No matching icons.')}</p>
              )}
            </>
          ) : (
            <>
              {/* The chosen mark is not in any section below — it is one of six
                  thousand theSVG slugs — so without this the dialog would say
                  nothing about what is currently set. */}
              {chosenSlug && (
                <Section label="theSVG">
                  <Tile label={chosenSlug} selected onClick={() => onChange({ category: value.category, icon: value.icon })}>
                    <DeviceIcon icon={value.icon} category={value.category} size="md" />
                  </Tile>
                </Section>
              )}
              {BRAND_GROUPS.map((group) => (
                <Section key={group.label} label={t(group.label)}>
                  {group.choices.map((choice) => (
                    <Tile key={choice.icon} label={choice.label} selected={selected(choice)} onClick={() => choose(choice)}>
                      <DeviceIcon icon={choice.icon} category={choice.category} size="md" />
                    </Tile>
                  ))}
                </Section>
              ))}
              <Section label={t('Common')}>
                {COMMON_TILES.map((tile) => (
                  <Tile key={tile.icon || tile.category} label={tile.label} selected={selected(tile)} onClick={() => choose(tile)}>
                    <DeviceIcon icon={tile.icon || undefined} category={tile.category} size="md" />
                  </Tile>
                ))}
              </Section>
            </>
          )}
        </div>
      </div>
    </div>
  )
}

function Section({ label, children }: { label: string; children: React.ReactNode }) {
  useLocale()
  return (
    <div className="space-y-2">
      <div className="text-xs font-medium text-muted-foreground">{label}</div>
      <div className="grid grid-cols-3 gap-1.5 sm:grid-cols-4">{children}</div>
    </div>
  )
}

function Tile({
  label,
  selected,
  onClick,
  children,
}: {
  label: string
  selected: boolean
  onClick: () => void
  children: React.ReactNode
}) {
  useLocale()
  return (
    <button
      type="button"
      onClick={onClick}
      aria-label={label}
      aria-pressed={selected}
      title={label}
      className={cn(
        'flex min-h-20 flex-col items-center justify-center gap-1 rounded-lg border px-1.5 py-2 transition-colors',
        selected ? 'border-primary bg-primary/5' : 'border-transparent hover:bg-accent/60',
      )}
    >
      {children}
      <span
        className={cn(
          'w-full truncate text-center text-[0.7rem] leading-tight',
          selected ? 'font-medium' : 'text-muted-foreground',
        )}
      >
        {label}
      </span>
    </button>
  )
}
