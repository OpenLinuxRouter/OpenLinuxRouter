import { t, useLocale } from '@/lib/i18n'
import { Trash2 } from 'lucide-react'
import { useState } from 'react'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { useDhcpConfig } from '@/features/dhcp/queries'
import type { Host } from '@/lib/config-types'

const EMPTY: Host = { name: '', addresses: [] }

/**
 * Naming one device.
 *
 * Two fields, and the preview line underneath the first is the reason this is a
 * dialog rather than two inline cells: the thing the operator is about to type
 * into a browser is the *qualified* name, and they have no way to know what it
 * will be unless it is shown to them as they type.
 */
export function HostDialog({
  open,
  onOpenChange,
  domain,
  taken,
  initial,
  onSubmit,
  onRemove,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** The suffix names are published under, for the preview line. */
  domain: string
  /** Names already published, so a device is only suggested once. */
  taken: string[]
  /** Undefined when adding. */
  initial?: Host
  onSubmit: (host: Host) => void
  onRemove?: () => void
}) {
  useLocale()
  const [draft, setDraft] = useState<Host>(initial ?? EMPTY)
  const editing = initial !== undefined

  const name = draft.name.trim()

  // Reserved devices that have a name and are not published yet.
  //
  // The join is done here, in the client, on purpose. A reservation and a local
  // name are two modules' configuration and there is no cross-module
  // transaction (design.md §5.3.1) — picking one of these fills two fields in
  // and still writes one module. olrd storing one from the other is the door
  // that stays shut; internal/dns/dhcp.go says why.
  const dhcp = useDhcpConfig()
  const suggestions = editing
    ? []
    : (dhcp.data?.reservations ?? [])
        .filter((r) => r.hostname && !taken.includes(relative(r.hostname, domain)))
        .slice(0, 6)

  // Only what the server requires. Whether the name is well-formed, whether the
  // address is on a network this box serves, whether two hosts collide — all of
  // that is the server's job (design.md §5.3.1) and is not duplicated here.
  const complete = name !== '' && draft.addresses.length > 0

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        onOpenChange(next)
        if (next) setDraft(initial ?? EMPTY)
      }}
    >
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{editing ? 'Edit name' : 'Add name'}</DialogTitle>
          <DialogDescription> {t('Resolve a local name to the specified addresses.')} </DialogDescription>
        </DialogHeader>

        <div className="grid gap-4">
          <div className="grid gap-2">
            <Label htmlFor="host-name">{t('Name')}</Label>
            <Input
              id="host-name"
              placeholder="sony-tv"
              value={draft.name}
              onChange={(e) => setDraft((d) => ({ ...d, name: e.target.value }))}
            />
            <p className="text-xs text-muted-foreground">
              {name ? (
                <> {t('Reachable as')}{' '}
                  <span className="font-mono">{qualify(name, domain)}</span> from
                  anywhere on this network.
                </>
              ) : (
                <> {t('Letters, digits and hyphens. It is published under')}{' '}
                  <span className="font-mono">{domain}</span>.
                </>
              )}
            </p>
          </div>

          {suggestions.length > 0 && (
            <div className="grid gap-2">
              <Label>{t('Reserved clients')}</Label>
              <div className="flex flex-wrap gap-2">
                {suggestions.map((r) => (
                  <Button
                    key={r.mac}
                    type="button"
                    variant="secondary"
                    size="sm"
                    onClick={() =>
                      setDraft({ name: r.hostname ?? '', addresses: [r.ip] })
                    }
                  >
                    {r.hostname}
                    <span className="text-muted-foreground">{r.ip}</span>
                  </Button>
                ))}
              </div>
              <p className="text-xs text-muted-foreground">
                These already have a fixed address from DHCP. Picking one fills
                both fields in so the address is not typed twice.
              </p>
            </div>
          )}

          <div className="grid gap-2">
            <Label htmlFor="host-addresses">{t('Address')}</Label>
            <Input
              id="host-addresses"
              placeholder="192.168.1.50"
              value={draft.addresses.join(', ')}
              onChange={(e) =>
                setDraft((d) => ({ ...d, addresses: splitList(e.target.value) }))
              }
            />
            <p className="text-xs text-muted-foreground">
              The device's address on this network. Give both an IPv4 and an IPv6
              one, comma separated, if it has both — a client that asks for IPv6
              first has to get the same machine back.
            </p>
          </div>
        </div>

        <DialogFooter>
          {onRemove && (
            <Button
              variant="danger"
              className="mr-auto"
              onClick={() => {
                onRemove()
                onOpenChange(false)
              }}
            >
              <Trash2 className="size-4" aria-hidden />{t('Remove')}</Button>
          )}
          <Button variant="quiet" onClick={() => onOpenChange(false)}>{t('Cancel')}</Button>
          <Button
            disabled={!complete}
            onClick={() => {
              onSubmit({ ...draft, name })
              onOpenChange(false)
            }}
          >
            {editing ? t('Save') : t('Add')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

/**
 * The stored form: relative to the local domain, lowercased, no trailing dot.
 *
 * A second copy of what normalizeHostName does in Go, and it earns its keep for
 * one reason: pasting a name in full when the relative one is already there has
 * to replace that entry, not add a second one the server would then refuse as a
 * duplicate. The server remains the authority — this only has to agree with it
 * about identity.
 */
export function relative(name: string, domain: string): string {
  const bare = name.trim().replace(/^\.+|\.+$/g, '').toLowerCase()
  return bare.endsWith(`.${domain}`) ? bare.slice(0, -(domain.length + 1)) : bare
}

/** The form somebody types into a browser. */
export function qualify(name: string, domain: string): string {
  return `${relative(name, domain)}.${domain}`
}

function splitList(value: string): string[] {
  return value
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean)
}
