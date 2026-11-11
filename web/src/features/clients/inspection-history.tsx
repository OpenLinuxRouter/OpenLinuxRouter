import { useEffect, useState } from 'react'
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Download } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { api, getToken } from '@/lib/api'
import { t, useLocale } from '@/lib/i18n'
import { formatBytes } from '@/lib/utils'

interface Entry { id: string; at: number; method: string; url: string; status: number; bytes: number }
interface History {
  entries: Entry[]
  next_before: string
  retained_bytes: number
  total_bytes: number
  retention_days: number
  max_bytes: number
}
interface Message {
  headers: { name: string; value: string }[]
  body: string
  body_bytes: number
  body_truncated: boolean
  body_binary: boolean
}
interface Detail extends Entry { request?: Message | null; response?: Message | null; error?: string }
interface Retention { days: number; max_bytes: number }

export function InspectionRetention() {
  useLocale()
  const client = useQueryClient()
  const query = useQuery({ queryKey: ['inspection', 'retention'], queryFn: () => api.get<Retention>('/api/inspection/retention') })
  const [draft, setDraft] = useState<{ days: string; gib: string } | null>(null)
  const days = draft?.days ?? String(query.data?.days ?? 30)
  const gib = draft?.gib ?? String((query.data?.max_bytes ?? 5368709120) / 1024 ** 3)
  const bytes = Math.round(Number(gib) * 1024 ** 3)
  const valid = Number.isInteger(Number(days)) && Number(days) >= 1 && Number(days) <= 365 && Number.isFinite(bytes) && bytes >= 1024 ** 2 && bytes <= 1024 ** 4
  const save = useMutation({
    mutationFn: () => api.put<Retention>('/api/inspection/retention', { days: Number(days), max_bytes: bytes }),
    onSuccess: async () => {
      setDraft(null)
      await client.invalidateQueries({ queryKey: ['inspection'] })
    },
  })

  return <form className="space-y-3" onSubmit={(event) => { event.preventDefault(); if (valid && !save.isPending) save.mutate() }}>
    <h3 className="text-sm font-medium">{t('Saved request retention')}</h3>
    <p className="text-sm text-muted-foreground">{t('Applies to saved requests across all clients on this router. Oldest requests are removed when either limit is reached.')}</p>
    {query.isError ? <p role="alert" className="text-sm text-destructive">{t('Could not load retention settings.')} {query.error.message} <Button type="button" size="sm" variant="quiet" onClick={() => void query.refetch()}>{t('Retry')}</Button></p> : <div className="grid grid-cols-2 items-end gap-3">
      <div className="space-y-2"><Label htmlFor="inspection-retention-days">{t('Days to keep')}</Label><Input id="inspection-retention-days" type="number" min={1} max={365} step={1} value={days} disabled={query.isPending || save.isPending} onChange={(event) => setDraft({ days: event.target.value, gib })} /></div>
      <div className="space-y-2"><Label htmlFor="inspection-retention-size">{t('Router request storage (GiB)')}</Label><Input id="inspection-retention-size" type="number" min={1 / 1024} max={1024} step="any" value={gib} disabled={query.isPending || save.isPending} onChange={(event) => setDraft({ days, gib: event.target.value })} /></div>
    </div>}
    <p className="text-xs text-muted-foreground">{t('1–365 days · 1 MiB–1 TiB shared across clients. Reducing limits can remove older saved requests immediately.')}</p>
    {save.isError && <p role="alert" className="text-sm text-destructive [overflow-wrap:anywhere]">{save.error.message}</p>}
    {save.isSuccess && !draft && <p role="status" className="text-sm text-muted-foreground">{t('Retention settings saved.')}</p>}
    <Button type="submit" size="sm" variant="secondary" disabled={!valid || !draft || query.isPending || query.isError || save.isPending}>{save.isPending ? t('Saving…') : t('Save')}</Button>
  </form>
}

function ArchivedMessage({ label, message }: { label: string; message?: Message | null }) {
  useLocale()
  return <section className="min-w-0 space-y-3">
    <h3 className="text-sm font-medium">{label}</h3>
    {!message ? <p className="text-sm text-muted-foreground">{t('No message captured.')}</p> : <>
      <details className="text-sm"><summary className="cursor-pointer">{t('Headers')}</summary><dl className="mt-2 space-y-1 font-mono text-xs [overflow-wrap:anywhere]">{message.headers?.map((header, index) => <div key={index}><dt className="inline font-medium">{header.name}: </dt><dd className="inline text-muted-foreground">{header.value}</dd></div>)}</dl></details>
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground"><span>{t('Body')} · {formatBytes(message.body_bytes)}</span>{message.body_truncated && <span>{t('Preview truncated. Download the saved flow for the full body.')}</span>}</div>
      {message.body_binary ? <p className="text-sm text-muted-foreground">{t('Binary body. Download the saved flow to inspect it in mitmproxy.')}</p> : message.body ? <pre className="max-h-72 overflow-auto whitespace-pre-wrap rounded-lg bg-muted p-3 font-mono text-xs [overflow-wrap:anywhere]">{message.body}</pre> : <p className="text-sm text-muted-foreground">{t('Empty body')}</p>}
    </>}
  </section>
}

export function InspectionHistory({ mac }: { mac: string }) {
  useLocale()
  const client = useQueryClient()
  const [search, setSearch] = useState('')
  const [filter, setFilter] = useState('')
  const [selected, setSelected] = useState<Entry | null>(null)
  const [confirmClear, setConfirmClear] = useState(false)
  useEffect(() => {
    const timer = window.setTimeout(() => setFilter(search.trim()), 300)
    return () => window.clearTimeout(timer)
  }, [search])
  const history = useInfiniteQuery({
    queryKey: ['inspection', 'history', mac, filter],
    initialPageParam: '',
    queryFn: ({ pageParam }) => api.get<History>(`/api/inspection/history?${new URLSearchParams({ mac, limit: '50', search: filter, ...(pageParam ? { before: pageParam } : {}) })}`),
    getNextPageParam: (page) => page.next_before || undefined,
    refetchInterval: 15000,
  })
  const detail = useQuery({
    queryKey: ['inspection', 'history-detail', mac, selected?.id],
    queryFn: () => api.get<Detail>(`/api/inspection/history/detail?${new URLSearchParams({ mac, id: selected!.id })}`),
    enabled: Boolean(selected),
  })
  const download = useMutation({
    mutationFn: async (entry: Entry) => {
      const token = getToken()
      const response = await fetch(`/api/inspection/history/flow?${new URLSearchParams({ mac, id: entry.id })}`, { headers: token ? { Authorization: `Bearer ${token}` } : {} })
      if (!response.ok) {
        if (response.status === 401) window.dispatchEvent(new Event('olr:unauthorized'))
        const body = await response.json().catch(() => null)
        throw new Error(body?.error?.message || t('Could not download the saved flow.'))
      }
      const blob = await response.blob()
      const url = URL.createObjectURL(blob)
      const anchor = document.createElement('a')
      anchor.href = url
      anchor.download = `inspection-${entry.id.replace(/[^a-zA-Z0-9_-]/g, '_')}.mitm`
      document.body.append(anchor)
      anchor.click()
      anchor.remove()
      window.setTimeout(() => URL.revokeObjectURL(url), 1000)
    },
  })
  const clear = useMutation({
    mutationFn: () => api.delete(`/api/inspection/history?${new URLSearchParams({ mac })}`),
    onSuccess: async () => {
      setConfirmClear(false)
      setSelected(null)
      await client.invalidateQueries({ queryKey: ['inspection', 'history'] })
      client.removeQueries({ queryKey: ['inspection', 'history-detail', mac] })
    },
  })
  const entries = [...new Map(history.data?.pages.flatMap((page) => page.entries ?? []).map((entry) => [entry.id, entry]) ?? []).values()]
  const metadata = history.data?.pages[0]

  return <div className="space-y-3">
    <div className="flex flex-wrap items-center gap-3">
      <Input type="search" className="min-w-0 flex-1 sm:max-w-sm" placeholder={t('Search saved requests')} aria-label={t('Search saved requests')} value={search} onChange={(event) => setSearch(event.target.value)} />
      <Button size="sm" variant="quiet" className="ml-auto" disabled={!metadata || metadata.retained_bytes === 0 || clear.isPending} onClick={() => { clear.reset(); setConfirmClear(true) }}>{t('Clear saved requests')}</Button>
    </div>
    {metadata && <p className="text-xs text-muted-foreground">{t('{client} for this client · Saved requests across this router: {total} / {limit} · up to {days} days', { device: formatBytes(metadata.retained_bytes), total: formatBytes(metadata.total_bytes), limit: formatBytes(metadata.max_bytes), days: metadata.retention_days })}</p>}
    {history.isError && <p role="alert" className="text-sm text-destructive [overflow-wrap:anywhere]">{t('Saved requests are unavailable.')} {history.error.message} <Button size="sm" variant="quiet" onClick={() => void history.refetch()}>{t('Retry')}</Button></p>}
    {history.isPending ? <p className="text-sm text-muted-foreground">{t('Loading saved requests…')}</p> : !history.isError && entries.length === 0 ? <p className="text-sm text-muted-foreground">{filter ? t('No saved requests match this search.') : t('No saved requests yet. Completed and failed captured requests appear here.')}</p> : null}
    {entries.length > 0 && <ul className="space-y-1">{entries.map((entry) => <li key={entry.id} className="flex items-center gap-2">
      <button type="button" className="min-h-11 min-w-0 flex-1 rounded-lg px-2 py-2 text-left hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring" onClick={() => { download.reset(); setSelected(entry) }}>
        <span className="flex flex-wrap items-baseline gap-x-3 gap-y-1 text-xs"><time dateTime={new Date(entry.at * 1000).toISOString()} className="text-muted-foreground tabular-nums">{new Date(entry.at * 1000).toLocaleString()}</time><span className="font-mono">{entry.method}</span><span className="tabular-nums">{entry.status || t('Failed')}</span><span className="ml-auto text-muted-foreground tabular-nums">{formatBytes(entry.bytes)}</span></span>
        <span className="mt-1 block text-sm [overflow-wrap:anywhere]">{entry.url}</span>
      </button>
      <Button size="icon" variant="quiet" disabled={download.isPending} aria-label={t('Download saved flow for {url}', { url: entry.url })} title={t('Download .mitm')} onClick={() => download.mutate(entry)}><Download className="size-4" /></Button>
    </li>)}</ul>}
    {download.isError && <p role="alert" className="text-sm text-destructive [overflow-wrap:anywhere]">{download.error.message}</p>}
    {history.hasNextPage && <Button size="sm" variant="secondary" disabled={history.isFetchingNextPage} onClick={() => void history.fetchNextPage()}>{history.isFetchingNextPage ? t('Loading…') : t('Load older requests')}</Button>}
    <p className="text-xs text-muted-foreground">{t('Saved after a captured request completes or fails. Requests still in progress may be lost if the proxy stops.')}</p>
    <Dialog open={Boolean(selected)} onOpenChange={(open) => { if (!open) setSelected(null) }}>
      <DialogContent className="sm:max-w-3xl">
        <DialogHeader><DialogTitle>{t('Saved request')}</DialogTitle><DialogDescription className="[overflow-wrap:anywhere]">{selected?.method} {selected?.url}</DialogDescription></DialogHeader>
        {detail.isPending ? <p className="text-sm text-muted-foreground">{t('Loading saved request…')}</p> : detail.isError ? <p role="alert" className="text-sm text-destructive [overflow-wrap:anywhere]">{detail.error.message} <Button size="sm" variant="quiet" onClick={() => void detail.refetch()}>{t('Retry')}</Button></p> : detail.data && <div className="space-y-6">
          {detail.data.error && <p className="text-sm text-destructive [overflow-wrap:anywhere]">{detail.data.error}</p>}
          <ArchivedMessage label={t('Request')} message={detail.data.request} />
          <ArchivedMessage label={t('Response')} message={detail.data.response} />
        </div>}
        {download.isError && <p role="alert" className="text-sm text-destructive [overflow-wrap:anywhere]">{download.error.message}</p>}
        <p className="text-xs text-muted-foreground">{t('Open the downloaded .mitm file in mitmproxy or mitmweb for full request and response content.')}</p>
        <DialogFooter><Button variant="secondary" disabled={!selected || download.isPending} onClick={() => selected && download.mutate(selected)}><Download className="size-4" />{download.isPending ? t('Downloading…') : t('Download .mitm')}</Button></DialogFooter>
      </DialogContent>
    </Dialog>
    <Dialog open={confirmClear} onOpenChange={(open) => { if (!clear.isPending) setConfirmClear(open) }}>
      <DialogContent><DialogHeader><DialogTitle>{t('Clear saved requests for this client?')}</DialogTitle><DialogDescription>{t('Deletes this client’s saved request and response content. Site activity and other clients’ history are kept. Inspection continues if it is on.')}</DialogDescription></DialogHeader>
        {clear.isError && <p role="alert" className="text-sm text-destructive [overflow-wrap:anywhere]">{clear.error.message}</p>}
        <DialogFooter><Button variant="quiet" disabled={clear.isPending} onClick={() => setConfirmClear(false)}>{t('Cancel')}</Button><Button variant="danger" disabled={clear.isPending} onClick={() => clear.mutate()}>{clear.isPending ? t('Clearing…') : t('Clear saved requests')}</Button></DialogFooter>
      </DialogContent>
    </Dialog>
  </div>
}
