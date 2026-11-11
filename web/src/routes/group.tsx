import { t, useLocale } from '@/lib/i18n'
import { Heading } from '@/components/ui/heading'
import { MetricValue } from '@/components/ui/metric-value'
import { FolderTree, Network } from 'lucide-react'
import { useState } from 'react'
import { Link, useParams } from 'react-router'
import { toast } from 'sonner'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { List, ListEmpty, ListRow } from '@/components/ui/list'
import { DeviceIcon } from '@/features/clients/client-icon'
import { ClientSettingRow } from '@/features/clients/client-setting-row'
import { groupOptions, subtreeOf } from '@/features/clients/group-tree'
import { lastHeardLabel, lastHeardTitle } from '@/features/clients/last-heard'
import { useClientList, useClientsConfig } from '@/features/clients/queries'
import { ApplyOutcome as GatewayOutcome } from '@/features/gateway/editor'
import { gatewayChange, useGatewayConfig } from '@/features/gateway/queries'
import { useGatewayApply } from '@/features/gateway/use-apply'
import { useQosConfig, useQosClientsApply, type Priority } from '@/features/qos/queries'

const FOLLOW = ' follow'
const DIRECT = ' direct'

export function GroupPage() {
  useLocale()
  const { name = '' } = useParams()
  const identity = useClientsConfig()
  const devices = useClientList()
  const gateway = useGatewayConfig()
  const qos = useQosConfig()
  const gatewayApply = useGatewayApply()
  const qosApply = useQosClientsApply()
  const [exit, setExit] = useState(FOLLOW)
  const [priority, setPriority] = useState<Priority>('normal')
  const [confirming, setConfirming] = useState<'exit' | 'priority' | null>(null)
  const [working, setWorking] = useState(false)
  const groups = identity.data?.groups ?? []
  const group = groups.find((g) => g.name === name)
  const descendants = subtreeOf(groups, name)
  const members = (devices.data?.clients ?? []).filter((d) => d.group && descendants.has(d.group))
  const direct = members.filter((d) => d.group === name).length
  const children = groupOptions(groups).filter((g) => g.parent === name)

  if (identity.isPending || devices.isPending) return <p className="py-16 text-sm text-muted-foreground">{t('Loading group…')}</p>
  if (identity.isError || devices.isError) return <State title={t('Could not load this group')} />
  if (!group) return <State title={t('Group not found')} />

  async function applyCurrentMembers() {
    if (!confirming) return
    const action = confirming
    setConfirming(null)
    setWorking(true)
    let applied = 0
    let active = 0
    try {
      for (const device of members) {
        if (action === 'exit') {
          const change = exit === FOLLOW ? gatewayChange.removeClient(device.mac) : gatewayChange.client(device.mac, exit === DIRECT ? '' : exit)
          // An inherited device has nothing to remove. Do not turn a no-op into a 404.
          if (exit === FOLLOW && !gateway.data?.clients?.some((d) => d.mac.toLowerCase() === device.mac.toLowerCase())) continue
          if (!await gatewayApply.submit(change)) break
        } else {
          const current = qos.data?.clients?.find((d) => d.mac.toLowerCase() === device.mac.toLowerCase())
          const result = await qosApply.mutateAsync({ mac: device.mac, priority, download_mbps: current?.download_mbps || 0, upload_mbps: current?.upload_mbps || 0 })
          if (result.status.active && !result.status.pending?.includes(device.mac)) active++
        }
        applied++
      }
      if (action === 'priority' && applied) {
        if (active === applied || priority === 'normal') toast.success(`Priority applied to ${applied} devices`)
        else toast.warning(`Priority saved for ${applied} devices`, { description: `${active} active; remaining devices are waiting for their LAN interface.` })
      }
      if (applied < members.length) toast.warning(`Stopped after ${applied} of ${members.length} devices`, { get description() { return t('Review the remaining clients before trying again.') } })
    } catch (error) {
      toast.error(`Stopped after ${applied} of ${members.length} devices`, { description: String(error) })
    } finally {
      setWorking(false)
    }
  }

  return <div className="space-y-4 pb-12">
    <Card className="gap-0 p-4 sm:p-6">
      <header className="flex min-h-20 items-center gap-4 border-b pb-4">
        <span className="flex size-16 shrink-0 items-center justify-center rounded-xl bg-muted"><FolderTree className="size-8" aria-hidden /></span>
        <Heading as="h1" size="lg" className="min-w-0 break-words">{group.name}</Heading>
      </header>
      <div className="mt-4 grid gap-y-8 lg:grid-cols-2 lg:gap-x-16">
        <dl className="min-w-0">
          <ClientSettingRow label={t('Parent')}>{group.parent ? <Link className="inline-flex min-h-11 max-w-full items-center rounded-md focus-visible:outline-2 focus-visible:outline-ring sm:min-h-9" to={`/groups/${encodeURIComponent(group.parent)}`}><Badge variant="secondary" className="whitespace-normal break-words">{group.parent}</Badge></Link> : 'Top-level group'}</ClientSettingRow>
          <ClientSettingRow label={t('Clients')}><MetricValue value={members.length} /></ClientSettingRow>
          <ClientSettingRow label={t('Directly here')}><MetricValue value={direct} /></ClientSettingRow>
          <ClientSettingRow label={t('Subgroups')}>{children.length ? <div className="flex flex-wrap justify-end gap-2">{children.map((child) => <Link key={child.name} className="inline-flex min-h-11 max-w-full items-center rounded-md focus-visible:outline-2 focus-visible:outline-ring sm:min-h-9" to={`/groups/${encodeURIComponent(child.name)}`}><Badge variant="secondary" className="whitespace-normal break-words">{child.name}</Badge></Link>)}</div> : t('None')}</ClientSettingRow>
        </dl>
        <div className="min-w-0 space-y-4">
          <dl>
            <ClientSettingRow label={t('Uplink')}>
              <div role="group" aria-label={t('Uplink for current clients')} className="inline-flex max-w-full flex-wrap justify-end rounded-lg bg-muted p-0.5">
                {[{ value: FOLLOW, get label() { return t('Default') } }, { value: DIRECT, get label() { return t('This router') } }, ...(gateway.data?.exits ?? []).map((e) => ({ value: e.name, label: e.name }))].map((option) => <Button key={option.value} size="sm" className="h-11 max-w-full whitespace-normal break-words px-2 sm:h-7" variant={exit === option.value ? 'secondary' : 'quiet'} aria-pressed={exit === option.value} disabled={!gateway.data || working} onClick={() => setExit(option.value)}>{option.label}</Button>)}
              </div>
            </ClientSettingRow>
            <ClientSettingRow label={t('Priority')}>
              <div role="group" aria-label="Priority for current devices" className="inline-flex max-w-full flex-wrap justify-end rounded-lg bg-muted p-0.5">
                {(['high', 'normal', 'low'] as const).map((value) => <Button key={value} size="sm" className="h-11 px-2 capitalize sm:h-7" variant={priority === value ? 'secondary' : 'quiet'} aria-pressed={priority === value} disabled={!qos.data || working} onClick={() => setPriority(value)}>{value}</Button>)}
              </div>
            </ClientSettingRow>
          </dl>
          <p className="text-sm text-muted-foreground">{t('Apply to')} {members.length} current devices, including subgroups. Existing choices are replaced; future members do not inherit these settings. Default follows each device’s network.</p>
          <div className="flex flex-wrap justify-end gap-2">
            <Button variant="secondary" disabled={!members.length || !gateway.data || working} onClick={() => setConfirming('exit')}>{t('Apply uplink')}</Button>
            <Button variant="secondary" disabled={!members.length || !qos.data || working} onClick={() => setConfirming('priority')}>{t('Save priority')}</Button>
          </div>
          <p className="text-sm text-muted-foreground">Priority is saved but not enforced in this build. Individual speed limits are preserved; edit them in device details.</p>
          {gateway.isError && <p role="alert" className="text-sm text-destructive">{t('Gateway settings are unavailable.')}</p>}
          {qos.isError && <p role="alert" className="text-sm text-destructive">{t('QoS settings are unavailable.')}</p>}
          <GatewayOutcome applier={gatewayApply} />
        </div>
      </div>
    </Card>

    <Card><CardHeader><CardTitle>{t('Clients in this group')}</CardTitle></CardHeader><CardContent className="space-y-4">
      <p className="text-sm text-muted-foreground">{t('Includes clients in subgroups.')}</p>
      {members.length ? <List>{members.map((device) => <ListRow key={device.mac} to={`/clients/${encodeURIComponent(device.mac)}`} title={device.name || device.mac} subtitle={`${device.group === name ? device.network || 'Network unknown' : `${device.group} · ${device.network || 'Network unknown'}`}`} leading={<DeviceIcon icon={device.icon} category={device.category} vendor={device.vendor} vendorKey={device.vendor_key} />} trailing={<span className="text-xs tabular-nums text-muted-foreground" title={lastHeardTitle(device)}>{lastHeardLabel(device)}</span>} />)}</List> : <ListEmpty>{t('No clients in this group or its subgroups yet.')}</ListEmpty>}
    </CardContent></Card>

    <Dialog open={confirming !== null} onOpenChange={(open) => !open && setConfirming(null)}><DialogContent><DialogHeader><DialogTitle>{t('Change')} {members.length} current devices?</DialogTitle><DialogDescription>{confirming === 'exit' ? 'Their existing Internet via overrides will be changed. Traffic may take a different path; established connections normally keep theirs.' : 'Their priorities will apply to traffic crossing this router. Queue updates may briefly interrupt queued packets.'} {t('New members will not inherit this setting.')}</DialogDescription></DialogHeader><DialogFooter><Button variant="quiet" onClick={() => setConfirming(null)}>{t('Cancel')}</Button><Button onClick={() => void applyCurrentMembers()}>{t('Continue')}</Button></DialogFooter></DialogContent></Dialog>
  </div>
}

function State({ title }: { title: string }) {
  useLocale()
  return <div className="space-y-3 py-16 text-center"><Network className="mx-auto size-8 text-muted-foreground" aria-hidden /><Heading as="h1" size="lg">{title}</Heading><Button variant="secondary" nativeButton={false} render={<Link to="/">{t('Back to overview')}</Link>} /></div>
}
