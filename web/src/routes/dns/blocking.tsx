import { t, useLocale } from '@/lib/i18n'
import { Plus } from 'lucide-react'
import { useState } from 'react'

import { SubPage } from '@/components/layout/sub-page'
import { Button } from '@/components/ui/button'
import { List, ListEmpty, ListRow } from '@/components/ui/list'
import { ApplyOutcome, useDnsEditor } from '@/features/dns/editor'
import { PolicyDialog } from '@/features/dns/policy-dialog'
import type { Policy } from '@/lib/config-types'

export function DnsBlockingPage() {
  useLocale()
  const { config, busy, change, applier, gate } = useDnsEditor()
  const [editing, setEditing] = useState<Policy | undefined>(undefined)
  const [open, setOpen] = useState(false)

  if (!config) return gate
  const policies = config.policies ?? []

  function upsert(policy: Policy) {
    if (!config) return
    const rest = policies.filter((p) => p.name !== policy.name)
    change({ ...config, policies: [...rest, policy] })
  }

  function remove(name: string) {
    if (!config) return
    change({ ...config, policies: policies.filter((p) => p.name !== name) })
  }

  return (
    <SubPage section="/gateway/dns" slug="blocking" action={<Button
          size="sm"
          disabled={busy}
          onClick={() => {
            setEditing(undefined)
            setOpen(true)
          }}
        >
          <Plus className="size-4" aria-hidden />{t('Add')}</Button>}>
      <ApplyOutcome applier={applier} />


      {policies.length === 0 ? (
        <ListEmpty>{t('No rules yet. Every client may look up anything.')}</ListEmpty>
      ) : (
        <List>
          {policies.map((p) => (
            <ListRow
              key={p.name}
              title={p.name}
              // Not a dash for the client-less rule: an operator scanning this
              // needs to see which one is the catch-all, and "—" reads as
              // "nobody" rather than "everybody".
              subtitle={
                p.clients?.length ? p.clients.join(', ') : 'Everyone not covered by another rule'
              }
              trailing={`${p.block?.length ?? 0} blocked${
                p.allow?.length
                  ? ` · ${p.allow.length} exception${p.allow.length === 1 ? '' : 's'}`
                  : ''
              }`}
              editLabel={`${t('Edit')} ${p.name}`}
              onSelect={
                busy
                  ? undefined
                  : () => {
                      setEditing(p)
                      setOpen(true)
                    }
              }
            />
          ))}
        </List>
      )}

      <PolicyDialog
        key={editing?.name ?? 'new'}
        open={open}
        onOpenChange={setOpen}
        initial={editing}
        onSubmit={upsert}
        onRemove={editing ? () => remove(editing.name) : undefined}
      />
    </SubPage>
  )
}
