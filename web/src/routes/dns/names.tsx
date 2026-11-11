import { t, useLocale } from '@/lib/i18n'
import { Plus } from 'lucide-react'
import { useState } from 'react'

import { SubPage } from '@/components/layout/sub-page'
import { Button } from '@/components/ui/button'
import { Disclosure } from '@/components/ui/disclosure'
import { EditableField } from '@/components/ui/editable-field'
import { List, ListEmpty, ListRow } from '@/components/ui/list'
import { ApplyOutcome, useDnsEditor } from '@/features/dns/editor'
import { HostDialog, qualify, relative } from '@/features/dns/host-dialog'
import type { Host } from '@/lib/config-types'

/** The suffix local names get when none is configured — DefaultLocalDomain in Go. */
const DEFAULT_LOCAL_DOMAIN = 'home.arpa'

export function DnsNamesPage() {
  useLocale()
  const { config, busy, change, applier, gate } = useDnsEditor()
  const [editing, setEditing] = useState<Host | undefined>(undefined)
  const [open, setOpen] = useState(false)

  if (!config) return gate
  const hosts = config.hosts ?? []
  const domain = config.local_domain || DEFAULT_LOCAL_DOMAIN

  function upsert(host: Host) {
    if (!config) return
    // Stored relative, so pasting a name in full replaces the entry that is
    // already there rather than adding a second one the server would refuse.
    const name = relative(host.name, domain)
    const rest = hosts.filter((h) => h.name !== name)
    change({ ...config, hosts: [...rest, { ...host, name }] })
  }

  function remove(name: string) {
    if (!config) return
    change({ ...config, hosts: hosts.filter((h) => h.name !== name) })
  }

  return (
    <SubPage section="/gateway/dns" slug="names" action={<Button
          size="sm"
          disabled={busy}
          onClick={() => {
            setEditing(undefined)
            setOpen(true)
          }}
        >
          <Plus className="size-4" aria-hidden />{t('Add')}</Button>}>
      <ApplyOutcome applier={applier} />


      {hosts.length === 0 ? (
        <ListEmpty>{t('No names yet. Clients here are reachable by address only.')}</ListEmpty>
      ) : (
        <List>
          {hosts.map((h) => (
            <ListRow
              wrap
              key={h.name}
              // The qualified name, not the stored relative one: this is the
              // string somebody is about to type into a browser.
              title={qualify(h.name, domain)}
              subtitle={h.addresses.join(', ')}
              editLabel={`${t('Edit')} ${qualify(h.name, domain)}`}
              onSelect={
                busy
                  ? undefined
                  : () => {
                      setEditing(h)
                      setOpen(true)
                    }
              }
            />
          ))}
        </List>
      )}

      <EditableField
        id="dns-local-domain"
        label={t('Published under')}
        busy={busy}
        placeholder={DEFAULT_LOCAL_DOMAIN}
        stored={config.local_domain ?? ''}
        hint={<>{t('Suffix for local names. Default:')}<span className="font-mono">{DEFAULT_LOCAL_DOMAIN}</span>.</>}
        onSave={(value) => change({ ...config, local_domain: value.trim() || undefined })}
      />

      <EditableField
        id="dns-private-domains"
        label={t('Private-answer zones')}
        busy={busy}
        placeholder="home.example.com"
        stored={(config.private_domains ?? []).join(', ')}
        hint={
          <>
            {t('Comma separated. Domains whose public records may point at an address on this network — in practice the domain you publish your own services under.')}{' '}
            {t('Without it, this resolver hides those answers for everyone using it.')}
          </>
        }
        onSave={(value) => {
          const list = value.split(/[,\s]+/).filter(Boolean)
          change({ ...config, private_domains: list.length ? list : undefined })
        }}
      />

      <Disclosure summary={t('About local domains')}>
        <p className="text-sm text-muted-foreground">
          home.arpa is reserved for home networks. Bare names also resolve when DHCP advertises this suffix as the search domain.
        </p>
      </Disclosure>

      <HostDialog
        key={editing?.name ?? 'new'}
        open={open}
        onOpenChange={setOpen}
        domain={domain}
        taken={hosts.map((h) => h.name)}
        initial={editing}
        onSubmit={upsert}
        onRemove={editing ? () => remove(editing.name) : undefined}
      />
    </SubPage>
  )
}
