import { t, useLocale } from '@/lib/i18n'
import { TechnicalValue } from '@/components/ui/property-row'
import { useRef, useState, type ReactNode } from 'react'
import { toast } from 'sonner'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { CategoryPicker, type IconChoice } from '@/features/clients/category-picker'
import { rememberedNameTitle } from '@/features/clients/client-name'
import { ClientHeader } from '@/features/clients/client-header'
import { DeviceIcon } from '@/features/clients/client-icon'
import { ClientSettingRow } from '@/features/clients/client-setting-row'
import { groupOptions } from '@/features/clients/group-tree'
import { useApplyClientsConfig, useClientsConfig } from '@/features/clients/queries'
import { ApplyOutcome as DhcpOutcome } from '@/features/dhcp/editor'
import { useDhcpConfig } from '@/features/dhcp/queries'
import { useDhcpApply } from '@/features/dhcp/use-apply'
import { ApplyOutcome as GatewayOutcome } from '@/features/gateway/editor'
import { gatewayChange, useGatewayConfig } from '@/features/gateway/queries'
import { useGatewayApply } from '@/features/gateway/use-apply'
import { ApiError } from '@/lib/api'
import type { ClientRow } from '@/lib/api-types'
import type { Client, ClientsConfig, Reservation } from '@/lib/config-types'

const NO_GROUP = ' none'
const INHERIT_EXIT = ' inherit'
const DIRECT_EXIT = ' direct'

type Field = 'name' | 'icon' | 'reservation' | 'group' | 'exit' | null

export function ClientInlineEditor({ device, traffic, activity, services, total }: { device: ClientRow; traffic: ReactNode; activity?: ReactNode; services?: ReactNode; total?: ReactNode }) {
  useLocale()
  const identity = useClientsConfig()
  const saveIdentity = useApplyClientsConfig()
  const dhcp = useDhcpConfig()
  const dhcpApplier = useDhcpApply()
  const gateway = useGatewayConfig()
  const gatewayApplier = useGatewayApply()
  const reservation = dhcp.data?.reservations?.find((r) => r.mac.toLowerCase() === device.mac.toLowerCase())
  const override = gateway.data?.devices?.find((r) => r.mac.toLowerCase() === device.mac.toLowerCase())
  const exit = override ? override.exit || DIRECT_EXIT : INHERIT_EXIT
  const [editing, setEditing] = useState<Field>(null)
  const [name, setName] = useState('')
  const nameFinished = useRef(false)
  const [look, setLook] = useState<IconChoice>({ category: '', icon: '' })
  const [ip, setIp] = useState('')
  const [fixed, setFixed] = useState(false)
  const [group, setGroup] = useState(NO_GROUP)
  const [uplink, setUplink] = useState(INHERIT_EXIT)
  const identityBusy = !identity.data || saveIdentity.isPending

  function begin(field: Exclude<Field, null>) {
    setEditing(field)
    nameFinished.current = false
    setName(device.name || device.mac)
    setLook({ category: device.category_origin === 'operator' ? device.category : '', icon: device.icon ?? '' })
    setIp(reservation?.ip ?? device.ips?.find((address) => !address.includes(':')) ?? '')
    setFixed(!!reservation)
    setGroup(device.group || NO_GROUP)
    setUplink(exit)
  }

  async function save(patch: Partial<Client>) {
    if (!identity.data) return
    const stored = identity.data.clients?.find((d) => d.mac.toLowerCase() === device.mac.toLowerCase())
    const entry: Client = { ...stored, ...patch, mac: device.mac }
    const rest = (identity.data.clients ?? []).filter((d) => d.mac.toLowerCase() !== device.mac.toLowerCase())
    const empty = !entry.name && !entry.category && !entry.icon && !entry.group && !entry.notes && !entry.model
    const next: ClientsConfig = { ...identity.data, clients: empty ? rest : [...rest, entry] }
    try {
      await saveIdentity.mutateAsync(next)
      setEditing(null)
      toast.success('Client saved')
    } catch (error) {
      nameFinished.current = false
      toast.error(error instanceof ApiError ? error.message : String(error))
    }
  }

  async function saveReservation() {
    if (!dhcp.data) return
    const rest = (dhcp.data.reservations ?? []).filter((r) => r.mac.toLowerCase() !== device.mac.toLowerCase())
    const entry: Reservation = { ...reservation, mac: device.mac, ip: ip.trim() }
    const applied = await dhcpApplier.submit({ ...dhcp.data, reservations: fixed ? [...rest, entry] : rest })
    if (applied) setEditing(null)
  }

  function finishName(discard = false) {
    if (nameFinished.current) return
    nameFinished.current = true
    if (discard || name.trim() === (device.name || device.mac)) {
      setEditing(null)
      return
    }
    void save({ name: name.trim() || undefined })
  }

  return <>
    <Card className="gap-0 p-4 sm:p-6">
    <header>
      <ClientHeader
        activity={activity}
        picture={<DeviceIcon icon={device.icon} category={device.category} vendor={device.vendor} vendorKey={device.vendor_key} size="lg" />}
        pictureBusy={identityBusy}
        onPicture={() => begin('icon')}
        note={rememberedNameTitle(device)}
        title={editing === 'name' ? <Input autoFocus aria-label={t('Client name')} className="h-auto w-80 max-w-full py-1 text-2xl font-semibold" value={name} disabled={saveIdentity.isPending} onChange={(e) => { nameFinished.current = false; setName(e.target.value) }} onBlur={() => finishName()} onKeyDown={(e) => { if (e.nativeEvent.isComposing) return; if (e.key === 'Enter' || e.key === 'Escape') { e.preventDefault(); finishName(e.key === 'Escape') } }} />
          : <button type="button" disabled={identityBusy} onClick={() => begin('name')} aria-label={t('Edit client name')} title={rememberedNameTitle(device)} className="w-full break-words rounded-md text-left hover:text-primary focus-visible:outline-2 focus-visible:outline-ring">{device.name || device.mac}</button>}
      />
    </header>
    <div className="mt-4 grid gap-y-8 lg:grid-cols-2 lg:gap-x-16 lg:gap-y-0">
      <div className="min-w-0 lg:row-span-5 lg:grid lg:grid-rows-subgrid">
        <dl className="text-sm lg:row-span-5 lg:grid lg:grid-rows-subgrid">
          <Fact label={t('Vendor')} value={device.vendor || 'Unknown'} />
          {/* The hardware address is the key every other screen uses, so it
              belongs in the facts with the rest of the identity rather than
              floating beside the title. */}
          <Fact label="MAC" value={device.mac} mono />
          <ClientSettingRow label="IP">
            <div className="flex flex-wrap items-center justify-end gap-2">
              <Tooltip><TooltipTrigger onClick={() => begin('reservation')} disabled={!dhcp.data || dhcpApplier.busy} aria-label={t('Edit IP address')} className="min-h-11 sm:min-h-9 min-w-0 rounded-sm text-right font-mono text-[0.8125rem] leading-5 hover:text-primary focus-visible:outline-2 focus-visible:outline-ring"><span className="break-all">{device.ips?.length ? device.ips.join(', ') : device.fixed_ip || 'None observed'}</span></TooltipTrigger><TooltipContent>{device.expires ? `DHCP lease ends ${new Date(device.expires).toLocaleString()}` : 'No expiring DHCP lease'}</TooltipContent></Tooltip>
              {reservation && <Badge variant="secondary">{t('Fixed')}</Badge>}
            </div>
          </ClientSettingRow>
          <ClientSettingRow label={t('Group')}><button type="button" aria-label={t('Edit client group')} disabled={identityBusy} onClick={() => begin('group')} className="min-h-11 rounded-md focus-visible:outline-2 focus-visible:outline-ring sm:min-h-9"><Badge variant="secondary" className="whitespace-normal break-words">{device.group || 'No group'}</Badge></button></ClientSettingRow>
          <ClientSettingRow label={t('Services')}>{services}</ClientSettingRow>
        </dl>
      </div>
      <div className="min-w-0 space-y-2 lg:contents lg:space-y-0">
        <div className="flex min-h-11 flex-wrap items-center justify-between gap-2 py-1 text-sm sm:min-h-9 lg:col-start-2 lg:row-start-1">
          <span className="text-muted-foreground">{t('Uplink')}</span>
          <div role="group" aria-label={t('Client uplink')} className="flex max-w-full flex-wrap justify-end rounded-lg bg-muted p-0.5">
            {[{ value: INHERIT_EXIT, get label() { return t('Default') } }, ...(exit === DIRECT_EXIT ? [{ value: DIRECT_EXIT, get label() { return t('This router’s connection') } }] : []), ...(gateway.data?.exits ?? []).map((e) => ({ value: e.name, label: e.name }))].map((option) => <Button key={option.value} size="sm" className="h-11 max-w-full whitespace-normal break-words px-2 sm:h-7" variant={exit === option.value ? 'secondary' : 'quiet'} aria-pressed={exit === option.value} disabled={!gateway.data || gatewayApplier.busy} onClick={() => { if (exit !== option.value) void gatewayApplier.submit(option.value === INHERIT_EXIT ? gatewayChange.removeDevice(device.mac) : gatewayChange.device(device.mac, option.value === DIRECT_EXIT ? '' : option.value)) }}>{option.label}</Button>)}
          </div>
        </div>
        {gateway.isError && <p className="text-sm text-destructive lg:col-start-2 lg:row-start-6">{t('Could not load uplinks.')}</p>}
        {traffic}
        <div className="flex min-h-11 flex-wrap items-center justify-between gap-3 py-1 text-sm sm:min-h-9 lg:col-start-2 lg:row-start-5"><span className="text-muted-foreground">{t('Total')}</span>{total}</div>
      </div>
    </div>
    </Card>
    <Dialog open={editing === 'icon'} onOpenChange={(open) => { if (!open) setEditing(null) }}>
      <DialogContent className="max-h-[90svh] overflow-y-auto">
        <DialogHeader><DialogTitle>{t('Client icon')}</DialogTitle></DialogHeader>
        <CategoryPicker value={look} onChange={setLook} />
        <DialogFooter><Button variant="quiet" disabled={saveIdentity.isPending} onClick={() => setEditing(null)}>{t('Cancel')}</Button><Button disabled={identityBusy} onClick={() => void save({ category: look.category || undefined, icon: look.icon || undefined })}>{t('Save icon')}</Button></DialogFooter>
      </DialogContent>
    </Dialog>
    <Dialog open={editing === 'reservation' && !dhcpApplier.confirming} onOpenChange={(open) => { if (!open && !dhcpApplier.busy) setEditing(null) }}>
      <DialogContent className="max-h-[90svh] overflow-y-auto">
        <DialogHeader><DialogTitle>{t('IP address')}</DialogTitle><DialogDescription>{t('Address settings for')} {device.name || device.mac}.</DialogDescription></DialogHeader>
        <form className="space-y-4" onSubmit={(event) => { event.preventDefault(); if (!dhcpApplier.busy && (!fixed || ip.trim())) void saveReservation() }}>
          <div className="space-y-2"><label htmlFor="device-ip" className="text-sm font-medium">{t('IP address')}</label><Input id="device-ip" autoFocus value={ip} disabled={dhcpApplier.busy} onChange={(e) => { setIp(e.target.value); setFixed(true) }} placeholder="192.168.1.50" /></div>
          <div className="flex items-center justify-between gap-3"><label htmlFor="device-fixed" className="text-sm font-medium">{t('Fixed IP address')}</label><Switch id="device-fixed" checked={fixed} busy={dhcpApplier.busy} onCheckedChange={setFixed} /></div>
          <DialogFooter><Button type="button" variant="quiet" disabled={dhcpApplier.busy} onClick={() => setEditing(null)}>{t('Cancel')}</Button><Button type="submit" disabled={(fixed && !ip.trim()) || !dhcp.data || dhcpApplier.busy}>{t('Save')}</Button></DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
    <Dialog open={editing === 'group' || (editing === 'exit' && !gatewayApplier.confirming)} onOpenChange={(open) => { if (!open && !saveIdentity.isPending && !gatewayApplier.busy) setEditing(null) }}>
      <DialogContent>
        <DialogHeader><DialogTitle>{editing === 'group' ? t('Group') : t('Uplink')}</DialogTitle><DialogDescription>{editing === 'group' ? 'Choose a group for this device.' : 'Choose the connection this device uses to reach the internet. Existing connections normally keep their path.'}</DialogDescription></DialogHeader>
        {editing === 'group' ? <Select value={group} disabled={identityBusy} onValueChange={(v) => { if (v) setGroup(v) }}><SelectTrigger aria-label={t('Client group')} className="w-full"><SelectValue>{(v: string) => v === NO_GROUP ? t('No group') : v}</SelectValue></SelectTrigger><SelectContent><SelectItem value={NO_GROUP}>{t('No group')}</SelectItem>{groupOptions(identity.data?.groups ?? []).map((g) => <SelectItem key={g.name} value={g.name}><span style={{ paddingLeft: g.depth * 12 }}>{g.name}</span></SelectItem>)}</SelectContent></Select> : <Select value={uplink} disabled={!gateway.data || gatewayApplier.busy} onValueChange={(v) => { if (v) setUplink(v) }}><SelectTrigger aria-label={t('Client uplink')} className="w-full"><SelectValue>{(v: string) => v === INHERIT_EXIT ? t('Default') : v === DIRECT_EXIT ? t('This router’s connection') : v}</SelectValue></SelectTrigger><SelectContent><SelectItem value={INHERIT_EXIT}>{t('Default')}</SelectItem>{(gateway.data?.exits ?? []).map((e) => <SelectItem key={e.name} value={e.name}>{e.name}</SelectItem>)}</SelectContent></Select>}
        <DialogFooter><Button variant="quiet" disabled={saveIdentity.isPending || gatewayApplier.busy} onClick={() => setEditing(null)}>{t('Cancel')}</Button><Button disabled={editing === 'group' ? identityBusy : !gateway.data || gatewayApplier.busy} onClick={async () => { if (editing === 'group') await save({ group: group === NO_GROUP ? undefined : group }); else if (await gatewayApplier.submit(uplink === INHERIT_EXIT ? gatewayChange.removeDevice(device.mac) : gatewayChange.device(device.mac, uplink))) setEditing(null) }}>{t('Save')}</Button></DialogFooter>
      </DialogContent>
    </Dialog>
    <DhcpOutcome applier={dhcpApplier} />
    <GatewayOutcome applier={gatewayApplier} />
  </>
}

function Fact({ label, value, mono = false }: { label: string; value: string; mono?: boolean }) {
  useLocale()
  return <ClientSettingRow label={label}><>{mono ? <TechnicalValue>{value}</TechnicalValue> : <span className="break-words">{value}</span>}</></ClientSettingRow>
}
