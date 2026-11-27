import { t, useLocale } from '@/lib/i18n'
import { AlertTriangle } from 'lucide-react'
import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { SubPage } from '@/components/layout/sub-page'
import { socksOutSummary } from '@/features/access/summary'
import { useSocksOutConfig, useSocksOutStatus } from '@/features/socksout/queries'
import type { SocksOutConfig } from '@/features/socksout/queries'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { api } from '@/lib/api'

export function SocksOutPage() {
  useLocale()
  const client = useQueryClient()
  const config = useSocksOutConfig()
  const status = useSocksOutStatus()
  const [draft, setDraft] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const save = useMutation({
    mutationFn: (next: SocksOutConfig) => api.put('/api/socksout/config', next),
    onSuccess: () => {
      setError(null)
      setDraft(null)
    },
    onError: (err: Error) => setError(err.message),
    // An apply can fail after storing intent or changing the service.
    onSettled: () => client.invalidateQueries({ queryKey: ['socksout'] }),
  })

  return (
    <SubPage section="/access" slug="socks5">
      {config.isError && <Alert variant="destructive"><AlertTriangle /><AlertTitle>{t('Could not load settings')}</AlertTitle><AlertDescription>{(config.error as Error).message}</AlertDescription></Alert>}
      {error && <Alert variant="destructive"><AlertTriangle /><AlertTitle>{t('Could not apply')}</AlertTitle><AlertDescription>{error}</AlertDescription></Alert>}
      {status.isError && <Alert><AlertTriangle /><AlertTitle>{t('Could not read proxy status')}</AlertTitle><AlertDescription>{status.error.message}</AlertDescription></Alert>}
      {status.data?.problem && <Alert><AlertTriangle /><AlertTitle>{t('Backend needs attention')}</AlertTitle><AlertDescription>{status.data.problem}</AlertDescription></Alert>}
      <Card>
        <CardHeader>
          <CardTitle>{t('Proxy connection')}</CardTitle>
          <CardDescription>{socksOutSummary(config, status).text}</CardDescription>
        </CardHeader>
        <CardContent className="space-y-3">
          <div className="max-w-xl space-y-2">
            <Label htmlFor="socks-out-proxy">{t('SOCKS5 server')}</Label>
            <Input id="socks-out-proxy" disabled={config.isPending || config.isError || save.isPending} aria-describedby="socks-out-proxy-hint" value={draft ?? config.data?.proxy ?? ''} onChange={(e) => setDraft(e.target.value)} placeholder="socks5://192.0.2.10:1080" autoComplete="off" spellCheck={false} />
            <p id="socks-out-proxy-hint" className="text-xs text-muted-foreground">{t('IPv4 address and port only. Hostnames and authentication are not supported.')}</p>
          </div>
          <div className="flex flex-wrap gap-2">
            <Button disabled={save.isPending || config.isPending || config.isError || !(draft ?? config.data?.proxy)?.trim()} onClick={() => save.mutate({ enabled: true, proxy: (draft ?? config.data?.proxy)?.trim() })}>{save.isPending ? t('Applying…') : 'Save and start'}</Button>
            {config.data?.enabled && <Button variant="secondary" disabled={save.isPending || config.isError} onClick={() => save.mutate({ ...config.data, enabled: false })}>{t('Stop')}</Button>}
          </div>
        </CardContent>
      </Card>
      <Card>
        <CardHeader><CardTitle>{t('Choose who uses it')}</CardTitle><CardDescription>Starting does not change client routes. Create a way out through <code>olrsocks0</code> and assign it to a network or client. IPv6 and backend failure block rather than bypass the proxy.</CardDescription></CardHeader>
        <CardContent><Button variant="secondary" nativeButton={false} render={<Link to="/access/gateway">{t('Open gateways')}</Link>} /></CardContent>
      </Card>
    </SubPage>
  )
}
