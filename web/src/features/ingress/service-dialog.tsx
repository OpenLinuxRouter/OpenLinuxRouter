import { t, useLocale } from '@/lib/i18n'
import { ExternalLink, Trash2 } from 'lucide-react'
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
import { Disclosure } from '@/components/ui/disclosure'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { ServiceIconPicker } from '@/features/ingress/service-icon-picker'
import { Textarea } from '@/components/ui/textarea'
import { useClientList } from '@/features/clients/queries'
import type { Service, ServiceExposure, UpstreamScheme } from '@/lib/config-types'

const EMPTY: Service = { name: '', upstream: { port: 0 } }

/** The sentinel for "somewhere that is not a device on this network". */
const MANUAL = ' manual'

/**
 * Publishes a service, or edits one.
 *
 * Two fields, which is the module's whole claim: a name and where requests go.
 * The certificate is not asked about because it was arranged once — one wildcard
 * covers every name that will ever exist. The name's DNS record is not asked
 * about either, because it is the operator's, in their own zone: this dialog
 * says what the name will be, and never claims to have created the record.
 *
 * The target is a **device reference**, not an address, which is the opposite of
 * the firewall's forward dialog and worth the contrast. A forward is a kernel
 * rule that has to exist at boot, before any lease exists; a published service is
 * resolved per request, so it can name the device and let another module say
 * where that device is (design.md §4.1). Nothing here copies an address.
 *
 * The consequence shapes the picker: a device with no *fixed* address is not
 * offered at all. The daemon refuses it — a published name pointing at a pool
 * address works until the lease turns over and then proxies to a stranger's
 * laptop — and offering a choice that is about to be rejected is worse than not
 * offering it.
 */
export function ServiceDialog({
  open,
  onOpenChange,
  initial,
  initialUpstream,
  domain,
  publicDomain,
  onSubmit,
  onRemove,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  initial?: Service
  initialUpstream?: Service['upstream']
  /** The suffix names live under, for the preview. This module's own setting. */
  domain?: string
  /** The suffix exposed services answer on, when the operator has set one. */
  publicDomain?: string
  onSubmit: (service: Service) => Promise<boolean>
  /** Only supplied when editing. Removing is an edit to the thing you opened. */
  onRemove?: () => void
}) {
  useLocale()
  const empty = initialUpstream ? { ...EMPTY, upstream: initialUpstream } : EMPTY
  const [draft, setDraft] = useState<Service>(initial ?? empty)
  const [expose, setExpose] = useState<ServiceExposure>(initial?.expose ?? 'lan')
  const [allowFrom, setAllowFrom] = useState((initial?.allow_from ?? []).join('\n'))
  const [target, setTarget] = useState<string>(initial?.upstream.client ?? initialUpstream?.client ?? MANUAL)
  const [saving, setSaving] = useState(false)
  const editing = initial !== undefined

  const devices = useClientList()
  const all = devices.data?.clients ?? []

  // Only devices with a reserved address, per the header. Sorted so the list is
  // stable between renders rather than in whatever order presence came back in.
  const offerable = all
    .filter((d) => d.fixed_ip && d.name)
    .sort((a, b) => a.name.localeCompare(b.name))

  // How many were left out, and why — said once under the picker. Without this
  // the list looks arbitrary to somebody who can see the device on the Clients
  // page and cannot find it here.
  const withoutFixed = all.filter((d) => !d.fixed_ip && d.name).length

  function upstream<K extends keyof Service['upstream']>(key: K, value: Service['upstream'][K]) {
    setDraft((d) => ({ ...d, upstream: { ...d.upstream, [key]: value } }))
  }

  function pickTarget(value: string | null) {
    const next = value ?? MANUAL
    setTarget(next)
    setDraft((d) => ({
      ...d,
      // Client and host are alternatives and the daemon refuses both at once, so
      // choosing one clears the other here rather than letting a stale value ride
      // along into a validation error about a field the operator cannot see.
      upstream: { ...d.upstream, client: next === MANUAL ? '' : next, host: '' },
    }))
  }

  const name = draft.name.trim()
  const allowedSources = allowFrom.split('\n').map((v) => v.trim()).filter(Boolean)
  const complete =
    name !== '' && draft.upstream.port > 0 && (target !== MANUAL || (draft.upstream.host ?? '') !== '') &&
    (!draft.link_path || /^\/(?!\/)[a-zA-Z0-9/_~.-]*$/.test(draft.link_path)) &&
    (expose === 'lan' || (!!publicDomain && (expose !== 'restricted' || allowedSources.length > 0))) &&
    (draft.upstream.request_headers ?? []).every((h) =>
      /^[!#$%&'*+.^_`|~0-9A-Za-z-]+$/.test(h.name) &&
      ![...(h.value ?? '')].some((c) => c.charCodeAt(0) < 32 || c.charCodeAt(0) === 127) && (!h.remove || !h.value),
    ) && new Set((draft.upstream.request_headers ?? []).map((h) => h.name.toLowerCase())).size ===
      (draft.upstream.request_headers ?? []).length

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next) {
          setDraft(initial ?? empty)
          setExpose(initial?.expose ?? 'lan')
          setAllowFrom((initial?.allow_from ?? []).join('\n'))
          setTarget(initial?.upstream.client ?? initialUpstream?.client ?? MANUAL)
        }
        onOpenChange(next)
      }}
    >
      <DialogContent className="max-h-[calc(100dvh-2rem)] overflow-y-auto sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{editing ? `Edit ${initial.name}` : 'Publish a service'}</DialogTitle>
          <DialogDescription> {t('Publish a network service at an HTTPS domain.')} </DialogDescription>
        </DialogHeader>

        <div className="space-y-4">
          <div className="space-y-1.5">
            <Label htmlFor="svc-name">{t('Address')}</Label>
            <div className="flex items-center gap-1.5">
              <Input
                id="svc-name"
                value={draft.name}
                placeholder="grafana"
                autoComplete="off"
                onChange={(e) => setDraft((d) => ({ ...d, name: e.target.value }))}
              />
              {/* The suffix shown rather than typed, so the whole address is
                  visible without retyping it on every service. It is set once,
                  in the certificate dialog above. */}
              <span className="shrink-0 font-mono text-sm text-muted-foreground">
                .{domain ?? '…'}
              </span>
            </div>
            <p className="text-xs text-muted-foreground">
              {name ? (
                <> {t('It will answer at')}{' '}
                  {editing && domain ? (
                    <a href={`https://${name}.${domain}${draft.link_path ?? ''}`} target="_blank" rel="noreferrer" className="inline-flex items-center gap-1 font-mono text-primary underline-offset-4 hover:underline">
                      https://{name}.{domain}{draft.link_path ?? ''}
                      <ExternalLink className="size-3" aria-hidden />
                    </a>
                  ) : (
                    <span className="font-mono">https://{name}.{domain ?? '…'}</span>
                  )}{' '}
                  {t('once a DNS record for that name points at this router. The record is yours to create in your own zone.')}
                </>
              ) : (
                t('One word. The rest of the address is the domain you publish under.')
              )}
            </p>
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="svc-target">{t('Where it runs')}</Label>
            <Select value={target} onValueChange={pickTarget}>
              <SelectTrigger id="svc-target">
                {/* A render function, not the bare value: SelectValue shows what
                    it is given, and the sentinel is a string nobody should ever
                    read. Without this the trigger says "manual". */}
                <SelectValue placeholder={t('Choose a client')}>
                  {(v: string) => (v === MANUAL ? t('Somewhere else…') : all.find((d) => d.mac === v)?.name || v)}
                </SelectValue>
              </SelectTrigger>
              <SelectContent>
                {offerable.map((d) => (
                  <SelectItem key={d.mac} value={d.mac}>
                    {d.name} — {d.fixed_ip}
                  </SelectItem>
                ))}
                <SelectItem value={MANUAL}>{t('Somewhere else…')}</SelectItem>
              </SelectContent>
            </Select>
            {target === MANUAL ? (
              <p className="text-xs text-muted-foreground">
                For something that is not a device on this network — a container on the router
                itself, or a machine behind another router.
              </p>
            ) : (
              <p className="text-xs text-muted-foreground">
                olr looks up where this device is each time, so the address below can change without
                breaking the link.
              </p>
            )}
          </div>

          {target === MANUAL && (
            <div className="space-y-1.5">
              <Label htmlFor="svc-host">{t('Address or hostname')}</Label>
              <Input
                id="svc-host"
                value={draft.upstream.host ?? ''}
                placeholder="127.0.0.1"
                autoComplete="off"
                onChange={(e) => upstream('host', e.target.value)}
              />
            </div>
          )}

          <div className="space-y-1.5">
            <Label htmlFor="svc-port">Port it listens on</Label>
            <Input
              id="svc-port"
              inputMode="numeric"
              value={draft.upstream.port || ''}
              placeholder="3000"
              onChange={(e) => upstream('port', Number(e.target.value.replace(/\D/g, '')) || 0)}
            />
            <p className="text-xs text-muted-foreground">
              The port you use today, the one in the address with the number in it.
            </p>
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="svc-link-path">{t('Page path (optional)')}</Label>
            <Input id="svc-link-path" value={draft.link_path ?? ''} placeholder="/ui"
              autoComplete="off" onChange={(e) => setDraft((d) => ({ ...d, link_path: e.target.value }))} />
            <p className="text-xs text-muted-foreground">
              Open this page and use its icon when the site's homepage has none. This does not change proxy routing.
            </p>
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="svc-expose">{t('Who can reach it')}</Label>
            <Select value={expose} onValueChange={(v) => setExpose(v as ServiceExposure)}>
              <SelectTrigger id="svc-expose">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="lan">{t('Only this network')}</SelectItem>
                <SelectItem value="public">{t('Anyone on the internet')}</SelectItem>
                <SelectItem value="restricted">{t('Only these addresses')}</SelectItem>
              </SelectContent>
            </Select>
            {expose === 'lan' ? (
              <p className="text-xs text-muted-foreground">
                {t('Answers on this network only. This is the default, and it is what every published address was before this setting existed.')}
              </p>
            ) : !publicDomain ? (
              <p className="text-xs text-amber-600 dark:text-amber-400">
                {t('Set a public domain in the settings first — without it there is no name for this to answer at, and no certificate.')}
              </p>
            ) : (
              <p className="text-xs text-muted-foreground">
                {t('It will also answer at')}{' '}
                <span className="font-mono">https://{name || '…'}.{publicDomain}</span>{' '}
                {expose === 'restricted'
                  ? t('from the addresses below.')
                  : t('from anywhere on the internet. That needs a DNS record for that name, and a port forward on the router in front of this box — olr cannot make either for you.')}
              </p>
            )}
          </div>

          {expose === 'restricted' && (
            <div className="space-y-1.5">
              <Label htmlFor="svc-allow">{t('Sources')}</Label>
              <Textarea
                id="svc-allow"
                rows={3}
                value={allowFrom}
                placeholder={'203.0.113.7\n198.51.100.0/24'}
                onChange={(e) => setAllowFrom(e.target.value)}
              />
              <p className="text-xs text-muted-foreground">
                {t('One address or CIDR per line. Every other source is refused, though the port still looks open to a scanner — only the service is protected.')}
              </p>
            </div>
          )}

          <ServiceIconPicker name={name} icon={draft.icon} onChange={(icon) => setDraft((d) => ({ ...d, icon }))} />

          {withoutFixed > 0 && target !== MANUAL && (
            // Said here rather than as a validation error later: the device
            // somebody is looking for is missing from the list above, and the
            // reason is a setting on a different page.
            <p className="text-xs text-muted-foreground">
              {withoutFixed === 1 ? 'One device is' : `${withoutFixed} devices are`} not listed
              because {withoutFixed === 1 ? 'it has' : 'they have'} no reserved address. A published
              address has to keep pointing at the same machine, so reserve one on the Clients page
              first.
            </p>
          )}

          <Disclosure summary={t('It only speaks https')}>
            <div className="space-y-1.5">
              <Label htmlFor="svc-scheme">{t('How olr reaches it')}</Label>
              <Select
                value={draft.upstream.scheme || 'http'}
                onValueChange={(v) => upstream('scheme', (v ?? 'http') as UpstreamScheme)}
              >
                <SelectTrigger id="svc-scheme">
                  <SelectValue>{(v: string) => (v === 'https' ? 'https' : 'http')}</SelectValue>
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="http">http</SelectItem>
                  <SelectItem value="https">https</SelectItem>
                </SelectContent>
              </Select>
              {/* The https case is the NAS and hypervisor UIs that refuse plain
                  HTTP. Their certificates are self-signed and are deliberately
                  not checked — over your own network, to a device you named,
                  demanding a valid one would make this option useless. */}
              <p className="text-xs text-muted-foreground">
                Leave this on http unless the service refuses it. The https a browser sees is
                created here either way.
              </p>
            </div>
          </Disclosure>
          <Disclosure summary={t('Upstream request headers')}>
            <div className="space-y-3">
              <p className="text-xs text-muted-foreground">
                Set or remove headers sent to this service. Browser response headers are unchanged.
                For a device that loops when accessed by its published name, set Host to its IP address.
                HTTPS SNI is configured separately from Host.
              </p>
              {(draft.upstream.request_headers ?? []).map((header, index) => (
                <div key={index} className="flex flex-wrap items-center gap-2">
                  <Input aria-label={`Header ${index + 1} name`} className="min-w-28 flex-1"
                    placeholder={t('Host')} value={header.name} onChange={(e) => {
                      const headers = [...(draft.upstream.request_headers ?? [])]
                      headers[index] = { ...header, name: e.target.value }
                      upstream('request_headers', headers)
                    }} />
                  <Input aria-label={`Header ${index + 1} value`} className="min-w-32 flex-1"
                    placeholder="172.16.1.163" disabled={header.remove} value={header.value ?? ''}
                    onChange={(e) => {
                      const headers = [...(draft.upstream.request_headers ?? [])]
                      headers[index] = { ...header, value: e.target.value }
                      upstream('request_headers', headers)
                    }} />
                  <label className="flex items-center gap-1 text-xs">
                    <input type="checkbox" checked={header.remove ?? false} onChange={(e) => {
                      const headers = [...(draft.upstream.request_headers ?? [])]
                      headers[index] = { ...header, remove: e.target.checked, value: e.target.checked ? '' : header.value }
                      upstream('request_headers', headers)
                    }} />{t('Remove')}</label>
                  <Button type="button" size="icon" variant="quiet" aria-label={`Delete header ${index + 1}`}
                    onClick={() => upstream('request_headers', (draft.upstream.request_headers ?? []).filter((_, i) => i !== index))}>
                    <Trash2 />
                  </Button>
                </div>
              ))}
              <Button type="button" size="sm" variant="secondary" onClick={() =>
                upstream('request_headers', [...(draft.upstream.request_headers ?? []), { name: '', value: '' }])}>{t('Add header')}</Button>
            </div>
          </Disclosure>
        </div>

        <DialogFooter className="sm:justify-between">
          {onRemove ? (
            <Button variant="danger" onClick={onRemove}>
              <Trash2 />{t('Stop publishing')}</Button>
          ) : (
            <span />
          )}
          <div className="flex gap-2">
            <Button variant="quiet" onClick={() => onOpenChange(false)}>{t('Cancel')}</Button>
            <Button
              disabled={!complete || saving}
              onClick={async () => {
                setSaving(true)
                let saved = false
                try {
                  saved = await onSubmit({
                    name,
                    link_path: draft.link_path?.trim() || undefined,
                    icon: draft.icon || undefined,
                    expose: expose === 'lan' ? undefined : expose,
                    allow_from: expose === 'restricted' ? allowedSources : undefined,
                    upstream: {
                      ...draft.upstream,
                      client: target === MANUAL ? undefined : target,
                      host: target === MANUAL ? (draft.upstream.host ?? '').trim() : undefined,
                      request_headers: draft.upstream.request_headers?.map((h) => ({
                        name: h.name.trim(), value: h.remove ? undefined : h.value ?? '', remove: h.remove || undefined,
                      })),
                    },
                  })
                } finally {
                  setSaving(false)
                }
                if (saved) onOpenChange(false)
              }}
            >
              {editing ? t('Save') : 'Publish'}
            </Button>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
