import { t, useLocale } from '@/lib/i18n'
import { StatusInline } from '@/components/ui/status'
import { List, ListEmpty, ListRow } from '@/components/ui/list'
import type { ForwardStatus } from '@/lib/api-types'
import type { Forward } from '@/lib/config-types'
import { formatBytes } from '@/lib/utils'

export function ForwardList({
  forwards,
  status,
  onEdit,
}: {
  forwards: Forward[]
  status?: ForwardStatus[]
  onEdit: (forward: Forward) => void
}) {
  useLocale()
  if (forwards.length === 0) {
    return (
      <ListEmpty> {t('No port forwards yet. Add a rule to publish a client port.')} </ListEmpty>
    )
  }

  return (
    <List>
      {forwards.map((f) => {
        const row = status?.find((s) => s.name === f.name)
        return (
          <ListRow
            key={f.name}
            title={f.name}
            subtitle={describeForward(f)}
            wrap
            trailing={row ? <ArrivedBadge row={row} /> : undefined}
            editLabel={`${t('Edit')} ${f.name}`}
            onSelect={() => onEdit(f)}
          />
        )
      })}
    </List>
  )
}

/**
 * Whether anything has arrived, which is the first question when a port does not
 * work — and it has three answers, not two.
 *
 * "Nothing yet" says the rule is in the kernel and no packet has matched it,
 * which points outward: the ISP, the modem, or somebody else's filter. "Not
 * counted" says we could not read the counter, which points at this box.
 * Collapsing them into a zero would send an operator to debug the wrong half.
 */
function ArrivedBadge({ row }: { row: ForwardStatus }) {
  useLocale()
  // "Not counted" is an answer we do not have; "nothing yet" and a byte total
  // are facts. Only the first is a status, so only it gets a dot.
  if (!row.counted) return <StatusInline tone="unknown">{t('Not counted')}</StatusInline>
  if (row.packets === 0) return <span className="text-muted-foreground">{t('Nothing yet')}</span>
  return <span className="tabular-nums">{formatBytes(row.bytes)} in</span>
}

/** The schema word for `both` is not a label; the other two already are. */
const PROTOCOL_LABEL: Record<string, string> = {
  '': 'TCP',
  tcp: 'TCP',
  udp: 'UDP',
  both: 'TCP+UDP',
}

function describeForward(f: Forward): string {
  const proto = PROTOCOL_LABEL[f.protocol ?? ''] ?? 'TCP'
  const hairpin = f.hairpin === false ? ' · not from inside' : ''

  return `${f.in} ${proto} ${f.port} → ${f.to}${hairpin}`
}
