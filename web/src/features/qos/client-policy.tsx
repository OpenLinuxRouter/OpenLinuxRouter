import { t, useLocale } from '@/lib/i18n'
import { useState, type ReactNode } from 'react'
import { toast } from 'sonner'
import { Meter } from '@/components/traffic-metrics'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { useQosConfig, useQosClientApply, type Priority } from '@/features/qos/queries'
import { formatRate } from '@/lib/utils'

export function ClientPolicy({ mac, downRate, upRate, children }: { mac: string; downRate?: number; upRate?: number; children?: ReactNode }) {
  useLocale()
  const config = useQosConfig()
  const apply = useQosClientApply(mac)
  const current = config.data?.clients?.find((d) => d.mac.toLowerCase() === mac.toLowerCase())
  const priority = current?.priority ?? 'normal'
  const [editing, setEditing] = useState<'download' | 'upload' | null>(null)
  const [limit, setLimit] = useState('')
  const valid = limit === '' || (Number.isFinite(Number(limit)) && Number(limit) > 0 && Number(limit) <= 100000)
  const busy = !config.data || apply.isPending

  async function save(nextPriority = priority) {
    if (!config.data) return
    try {
      const result = await apply.mutateAsync({ mac, priority: nextPriority,
        download_mbps: editing === 'download' ? Number(limit) : current?.download_mbps ?? 0,
        upload_mbps: editing === 'upload' ? Number(limit) : current?.upload_mbps ?? 0 })
      setEditing(null)
      const pending = result.status.pending?.includes(mac.toLowerCase())
      if (pending) toast.warning('Policy saved', { get description() { return t('Waiting for the client’s LAN interface to be identified.') } })
      else if (result.status.active) toast.success('Policy applied', { description: result.status.reason })
      else if (!result.config.clients?.some((d) => d.mac === mac.toLowerCase())) toast.success('Priority and limits reset')
      else toast.warning('Policy saved, but not active', { description: result.status.reason })
    } catch (error) {
      toast.error(String(error))
    }
  }

  function begin(direction: 'download' | 'upload') {
    setLimit(String((direction === 'download' ? current?.download_mbps : current?.upload_mbps) || ''))
    setEditing(direction)
  }

  return <section aria-label="Priority and traffic" className="space-y-3 lg:contents lg:space-y-0">
    <div className="flex min-h-11 flex-wrap items-center justify-between gap-3 py-1 text-sm sm:min-h-9 lg:col-start-2 lg:row-start-2">
      <span className="text-muted-foreground">{t('Priority')}</span>
      <div role="group" aria-label={t('Client priority')} className="flex rounded-lg bg-muted p-0.5">
        {(['high', 'normal', 'low'] as Priority[]).map((value) => <Button key={value} size="sm" className="h-11 px-2 sm:h-7" variant={priority === value ? 'secondary' : 'quiet'} aria-pressed={priority === value} disabled={busy} onClick={() => void save(value)}>{value[0].toUpperCase() + value.slice(1)}</Button>)}
      </div>
    </div>
    {config.isError && <p className="text-sm text-destructive lg:col-start-2 lg:row-start-6">{t('Could not load priority and limits.')}</p>}
    <div className="space-y-1 lg:contents lg:space-y-0">
      <div className="grid min-h-11 grid-cols-[6rem_minmax(0,1fr)] items-center gap-3 py-1 text-sm sm:min-h-9 sm:[&>div]:w-1/2 sm:[&>div]:justify-self-end lg:col-start-2 lg:row-start-3">
        <span className="text-muted-foreground">{t('Download')}</span>
        <Meter label={t('Download')} current={downRate === undefined ? '—' : formatRate(downRate)} maximum={current?.download_mbps ? String(current.download_mbps) + ' Mbps' : undefined} onClick={() => begin('download')} />
      </div>
      <div className="grid min-h-11 grid-cols-[6rem_minmax(0,1fr)] items-center gap-3 py-1 text-sm sm:min-h-9 sm:[&>div]:w-1/2 sm:[&>div]:justify-self-end lg:col-start-2 lg:row-start-4">
        <span className="text-muted-foreground">{t('Upload')}</span>
        <Meter label={t('Upload')} current={upRate === undefined ? '—' : formatRate(upRate)} maximum={current?.upload_mbps ? String(current.upload_mbps) + ' Mbps' : undefined} tone="amber" onClick={() => begin('upload')} />
      </div>
    </div>
    {children && <div className="lg:col-start-2 lg:row-start-7">{children}</div>}
    <Dialog open={!!editing} onOpenChange={(open) => { if (!open && !apply.isPending) setEditing(null) }}>
      <DialogContent>
        <DialogHeader><DialogTitle>{editing === 'download' ? 'Download limit' : 'Upload limit'}</DialogTitle><DialogDescription>{t('Applies immediately to traffic crossing this router. Leave blank for unlimited.')}</DialogDescription></DialogHeader>
        <form className="space-y-4" onSubmit={(event) => { event.preventDefault(); if (valid && !busy) void save() }}>
          <div className="space-y-2"><Label htmlFor="client-speed-limit">{t('Limit (Mbps)')}</Label><Input id="client-speed-limit" autoFocus type="number" min="0.001" max="100000" step="any" value={limit} placeholder={t('Unlimited')} disabled={busy} onChange={(event) => setLimit(event.target.value)} /></div>
          <DialogFooter><Button type="button" variant="quiet" disabled={apply.isPending} onClick={() => setEditing(null)}>{t('Cancel')}</Button><Button type="submit" disabled={busy || !valid}>{t('Save')}</Button></DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  </section>
}
