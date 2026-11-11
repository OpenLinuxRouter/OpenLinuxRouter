import { t, useLocale } from '@/lib/i18n'
import { ListEmpty } from '@/components/ui/list'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { gatewayChange, type GatewayChange } from '@/features/gateway/queries'
import type { AssignmentStatus } from '@/lib/api-types'
import type { GatewayConfig, Network } from '@/lib/config-types'

// Sentinel values for the two choices that are not an exit name.
//
// The **leading space is load-bearing** and is not a typo to tidy away: an exit
// name is trimmed by Normalize and required non-empty by Validate, so no real
// name can begin with one, which is what makes these impossible to collide with
// an exit somebody actually called "inherit". The empty string is not available
// — the Select treats it as "nothing chosen" — so a distinguishable non-empty
// value is needed for each.
export const INHERIT = ' inherit'
export const DIRECT = ' direct'

export /**
 * One row per network member, each with the sentence docs/gateway.md §1.3 settles on.
 *
 * The effective value carries its source, which is the whole reason
 * inheritance is usable here at all: *Proxy · from the box-wide setting* tells
 * an operator both what is happening and where to go to change it, without
 * anyone having to simulate a rule list.
 */
function NetworkList({
  config,
  networks,
  status,
  busy,
  onChange,
}: {
  config: GatewayConfig
  networks: Network[]
  status?: AssignmentStatus[]
  busy: boolean
  onChange: (change: GatewayChange) => void
}) {
  useLocale()
  const assignments = config.interfaces ?? []
  const exits = config.exits ?? []

  const members = networks.flatMap((network) =>
    network.members.map((iface) => ({ network: network.name, iface })),
  )

  if (members.length === 0) {
    return (
      <ListEmpty>
        No network interfaces yet. Add a network under Networks to choose how its clients reach the internet.
      </ListEmpty>
    )
  }

  function set(iface: string, value: string | null) {
    // An inherited row has no stored assignment; deleting an override returns
    // it to the same state without leaving an empty row behind.
    if (!value || value === INHERIT) {
      if (assignments.some((a) => a.interface === iface)) {
        onChange(gatewayChange.removeAssignment(iface))
      }
      return
    }
    onChange(gatewayChange.assign(iface, value))
  }

  // Not a List: these rows carry an interactive control, and List's trailing
  // slot is `hidden sm:block` — which would leave the setting unreachable on a
  // phone, on the one screen whose whole purpose is changing it.
  return (
    <ul className="divide-y overflow-hidden rounded-xl border">
      {members.map(({ network, iface }) => {
        const assignment = assignments.find((a) => a.interface === iface)
        const row = status?.find((s) => s.interface === iface)
        const inherited = config.default || 'this router’s own connection'
        const effective = assignment?.exit || config.default || ''
        const current =
          row?.exit === effective &&
          row.source === (assignment?.exit ? 'interface' : 'default')
        return (
          <li
            key={iface}
            className="flex flex-col gap-2 bg-card px-4 py-3 sm:flex-row sm:items-center sm:gap-3"
          >
            <div className="min-w-0 flex-1">
              <div className="truncate text-sm font-medium">{network}</div>
              <div className="truncate text-xs text-muted-foreground">{t('On')}{iface}</div>
              <div className="truncate text-[0.8rem] text-muted-foreground">
                {describeAssignment(current ? row : undefined, effective, Boolean(assignment?.exit))}
              </div>
            </div>
            <Select
              value={assignment?.exit || INHERIT}
              disabled={busy}
              onValueChange={(v) => set(iface, v)}
            >
              <SelectTrigger
                className="w-full sm:w-56"
                aria-label={`Internet via, for ${network} on ${iface}`}
              >
                <SelectValue>
                  {(v: string) => (v === INHERIT ? `Follow the setting above (${inherited})` : v)}
                </SelectValue>
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={INHERIT}>{t('Follow the setting above')}</SelectItem>
                {exits.map((e) => (
                  <SelectItem key={e.name} value={e.name}>
                    {e.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </li>
        )
      })}
    </ul>
  )
}

function describeAssignment(
  row: AssignmentStatus | undefined,
  exit: string,
  overridden: boolean,
): string {
  if (row?.reason) return row.reason
  const via = row?.exit || exit || 'this router’s own connection'
  return overridden ? via : `${via} — from the setting above`
}
