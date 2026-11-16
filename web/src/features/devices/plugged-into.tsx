import { t, useLocale } from '@/lib/i18n'
import { ChevronRight } from 'lucide-react'

import { Card } from '@/components/ui/card'
import { Heading } from '@/components/ui/heading'
import { Button } from '@/components/ui/button'
import { api } from '@/lib/api'

import { useDevicesConfig } from '@/features/devices/queries'
import type { Device, FDBResult } from '@/features/devices/types'
import { useMutation } from '@tanstack/react-query'
import { useState } from 'react'
import { Link } from 'react-router'

/**
 * Which switch port learned this address.
 *
 * A forwarding table is single-ended evidence: the switch says it saw the
 * address arrive on that port. That is not the same as a cable, and the caption
 * says so — a device behind a second switch is learned on the uplink, and an
 * address the table has aged out is absent rather than disconnected.
 *
 * It is a button, not a page load, because each lookup is an HTTP conversation
 * with one switch on a serialized lane: asking three switches can take seconds,
 * and a page that did it on arrival would make opening a client feel broken.
 */
export function PluggedInto({ mac }: { mac: string }) {
  useLocale()
  const inventory = useDevicesConfig()
  const [results, setResults] = useState<Outcome[] | null>(null)

  // A switch is anything we might ask: an explicit switch role, or the adapter we
  // know implements the lookup even when the operator left roles empty.
  const switches = (inventory.data?.devices ?? []).filter(device =>
    device.roles?.includes('switch') || device.adapter === 'mercury')

  const lookup = useMutation({
    mutationFn: async () => {
      const out: Outcome[] = []
      for (const device of switches) {
        try {
          const result = await api.get<FDBResult>(`/api/devices/${encodeURIComponent(device.id)}/fdb?${new URLSearchParams({ mac })}`)
          const entry = result.entries.find(candidate => candidate.mac.toLowerCase() === mac.toLowerCase())
          out.push(entry
            ? { kind: 'found', device, port: entry.port_id, vlan: entry.vlan, at: result.observed_at }
            : { kind: 'absent', device })
        } catch (error) {
          out.push({ kind: 'failed', device, reason: error instanceof Error ? error.message : t('No answer') })
        }
        setResults([...out])
      }
      return out
    },
    onSuccess: setResults,
  })

  if (switches.length === 0) return null

  return (
    <Card className="gap-0 py-0">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1 px-4 pt-3.5">
        <Heading as="h2" size="sm">{t('Plugged into')}</Heading>
        <span className="text-xs text-muted-foreground">{t('from the switches you manage')}</span>
        <div className="ml-auto">
          <Button variant="secondary" size="sm" onClick={() => { setResults(null); lookup.mutate() }} disabled={lookup.isPending}>
            {lookup.isPending ? t('Asking…') : results ? t('Ask again') : t('Look up port')}
          </Button>
        </div>
      </div>

      <div className="px-4 pt-2 pb-3.5">
        {results === null
          ? <p className="text-xs leading-5 text-muted-foreground">
              {t('Ask the switches which port learned this address. Each one is a separate read.')}
            </p>
          : <ul className="divide-y divide-border/60">
              {results.map(outcome => (
                <li key={outcome.device.id}>
                  {/* A port we found is a place to go: the device page opens on
                      the panel with that port selected. */}
                  {outcome.kind === 'found'
                    ? <Link to={`/devices/${encodeURIComponent(outcome.device.id)}?tab=ports`}
                        className="flex items-center gap-2 rounded-md py-2 text-sm hover:bg-accent/40 focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-ring">
                        <span className="min-w-0 flex-1 truncate">{outcome.device.name || outcome.device.id}</span>
                        <span className="text-right text-xs font-medium">{t('port {port}', { port: outcome.port })}</span>
                        {outcome.vlan ? <span className="text-xs text-muted-foreground">VLAN {outcome.vlan}</span> : null}
                        <ChevronRight aria-hidden className="size-3.5 text-border" />
                      </Link>
                    : <div className="flex items-center gap-2 py-2 text-sm">
                        <span className="min-w-0 flex-1 truncate">{outcome.device.name || outcome.device.id}</span>
                        <span className="text-right text-xs">
                          {outcome.kind === 'absent'
                            ? <span className="text-muted-foreground">{t('not in this switch’s table')}</span>
                            : <span className="text-warning-foreground">{t('could not read: {reason}', { reason: outcome.reason })}</span>}
                        </span>
                      </div>}
                </li>
              ))}
            </ul>}

        <p className="mt-2 text-xs leading-4 text-muted-foreground">
          {t('A forwarding table is one end’s word: the switch saw the address arrive on that port. A device behind another switch is learned on the uplink, and an aged-out address is absent rather than disconnected.')}
        </p>
      </div>
    </Card>
  )
}

type Outcome =
  | { kind: 'found'; device: Device; port: string; vlan?: number; at: string }
  | { kind: 'absent'; device: Device }
  | { kind: 'failed'; device: Device; reason: string }
