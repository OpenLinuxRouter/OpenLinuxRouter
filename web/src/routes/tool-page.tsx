import { t, useLocale } from '@/lib/i18n'
import { Activity, Copy, Globe, LoaderCircle, Radio, Route } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { PageHeading } from '@/components/layout/page-heading'
import { MetricValue } from '@/components/ui/metric-value'
import { TOOL_DEFINITIONS, type ToolSlug } from '@/features/tools/catalog'
import { behaviorLabel, bindingLabel, filteringVerdict, mappingVerdict, natTypeLabel, natTypeMeaning } from '@/features/tools/nat'
import { SpeedDial } from '@/features/tools/speed-dial'
import { useMutation, useQuery } from '@tanstack/react-query'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Disclosure } from '@/components/ui/disclosure'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { useGatewayConfig } from '@/features/gateway/queries'
import { ApiError, api } from '@/lib/api'

interface TestError {
  title: string
  guidance: string
  detail: string
}

function explainFailure(err: unknown): TestError {
  const detail = err instanceof Error ? err.message : String(err)
  if (err instanceof ApiError && err.status === 409) {
    return { get title() { return t('A test is already running') }, guidance: 'Wait for it to finish before starting another.', detail }
  }
  if (err instanceof ApiError && err.status === 401) {
    return { get title() { return t('Authentication required') }, guidance: 'Sign in to the router and try again.', detail }
  }
  if (err instanceof ApiError && err.status === 503) {
    return {
      get title() { return t('Speed test unavailable') },
      guidance: 'The router could not complete a test with the selected service. Check its internet connection, then try again later. This does not mean your network is down.',
      detail,
    }
  }
  return {
    get title() { return t('Could not reach the speed test') },
    guidance: 'Check that this page can still reach the router, then try again. Other router settings are unaffected.',
    detail,
  }
}

interface LANTest {
  addresses: string[]
  address?: string
  expires_at?: string
  port: number
  external?: Record<string, string>
  conflicts?: Record<string, string>
}

interface SpeedResult {
  server: string
  exit?: string
  location: string
  latency_ms: number
  jitter_ms: number | null
  download_mbps: number
  upload_mbps: number
}

/**
 * What the running test has observed so far, as the daemon reports it.
 *
 * `live_mbps` is the rate the transfer in flight is reaching, and the daemon
 * drops it the moment that transfer ends, so a number here is either a settled
 * measurement or one still moving — never a guess at where it will land.
 */
interface SpeedProgress {
  running: boolean
  provider?: string
  exit?: string
  phase?: string
  server?: string
  location?: string
  elapsed_ms: number
  latency_ms: number | null
  download_mbps: number | null
  upload_mbps: number | null
  live_mbps?: number
  error?: string
}

interface SpeedProvider {
  id: string
  name: string
  available: boolean
  detail?: string
}

interface ServerOption {
  id: string
  sponsor: string
  location: string
}

interface ServerList {
  servers: ServerOption[]
  total: number
}

interface NATResult {
  server: string
  /** NatTypeTester's RFC 3489 verdict, as a machine token. */
  nat_type: string
  mapped: string
  local?: string
  binding?: string
  mapping: string
  filtering: string
  /** One upstream probe did not finish while the other still produced evidence. */
  error?: string
}

interface PingResult {
  target: string
  address: string
  sent: number
  received: number
  loss_percent: number
  min_ms: number
  avg_ms: number
  max_ms: number
}

interface TraceResult {
  target: string
  output: string
  map_url?: string
}

function TraceView({ trace }: { trace: TraceResult }) {
  useLocale()
  const [mapAvailable, setMapAvailable] = useState(false)
  const [checkingMap, setCheckingMap] = useState(!!trace.map_url)

  useEffect(() => {
    if (!trace.map_url) return
    const controller = new AbortController()
    const timeout = window.setTimeout(() => controller.abort(), 8000)
    fetch(trace.map_url, { signal: controller.signal, mode: 'cors' })
      .then(async response => {
        if (!response.ok || !response.headers.get('content-type')?.includes('text/html')) return false
        const page = await response.text()
        // The viewer also serves its generic search page for expired trace IDs.
        return page.includes('property="og:title"') && !page.includes('Global Route Trace — MTR')
      })
      .then(setMapAvailable)
      .catch(() => setMapAvailable(false))
      .finally(() => {
        window.clearTimeout(timeout)
        setCheckingMap(false)
      })
    return () => {
      window.clearTimeout(timeout)
      controller.abort()
    }
  }, [trace.map_url])

  return <div className="mt-6" aria-live="polite">
    {checkingMap && <p role="status" className="text-sm text-muted-foreground">{t('Checking route map availability…')}</p>}
    {mapAvailable ? <div className="overflow-hidden rounded-xl border">
      <iframe title={`Route map to ${trace.target}`} src={trace.map_url} referrerPolicy="no-referrer" onError={() => setMapAvailable(false)} className="h-[60vh] min-h-[440px] w-full bg-white sm:h-[650px]" />
    </div> : !checkingMap && <>
      {trace.map_url && <p className="mb-3 text-xs text-muted-foreground">{t('Route map unavailable here.')} <a className="text-primary underline underline-offset-4" href={trace.map_url} target="_blank" rel="noopener noreferrer">{t('Open it in a new tab ↗')}</a></p>}
      <pre className="overflow-x-auto rounded-xl border bg-muted/40 p-4 font-mono text-xs leading-relaxed select-text">{trace.output}</pre>
    </>}
  </div>
}

export function ToolPage({ tool }: { tool: ToolSlug }) {
  useLocale()
  const definition = TOOL_DEFINITIONS.find(item => item.slug === tool)!
  const Content = {
    speedtest: OoklaSpeedTest,
    cloudflare: CloudflareSpeedTest,
    fastcom: FastComSpeedTest,
    librespeed: LibreSpeed,
    iperf3: IPerf3,
    nat: NATTest,
    ping: PingTest,
    edge: CDNEdgeTest,
    'dns-exit': DNSExitTest,
    'split-check': SplitCheckTest,
    traceroute: TracerouteTest,
  }[tool]
  return <div className="space-y-8">
    <PageHeading id="tool-title" title={definition.label} />
    <Content key={tool} />
  </div>
}

/** An internet speed test service that has a page of its own. */
type SpeedService = 'ookla' | 'cloudflare' | 'fastcom'

/** The service's own name, for the states its page reports about it. */
const SPEED_SERVICE_NAMES: Record<SpeedService, string> = {
  ookla: 'Speedtest by Ookla',
  cloudflare: 'Cloudflare Speed Test',
  fastcom: 'Fast.com',
}

function OoklaSpeedTest() { return <SpeedTest service="ookla" /> }
function CloudflareSpeedTest() { return <SpeedTest service="cloudflare" /> }
function FastComSpeedTest() { return <SpeedTest service="fastcom" /> }

/** Where the numbers came from: the server the service chose, and the way out. */
function resultSummary(service: SpeedService, result: SpeedResult): string {
  const server = service === 'ookla'
    ? `${t('Test server:')} ${result.server} · ${result.location}`
    : service === 'cloudflare' && result.location ? `${t('Cloudflare edge:')} ${result.location}` : ''
  const exit = `${t('Way out:')} ${result.exit || t('Router default route')}`
  return server ? `${server} · ${exit}` : exit
}

/** What the run is doing, named for the operator rather than for the library. */
function speedPhaseLabel(phase: string | undefined): string {
  switch (phase) {
    case 'server': return t('Finding a test server…')
    case 'latency': return t('Measuring latency…')
    case 'download': return t('Measuring download…')
    case 'upload': return t('Measuring upload…')
    // The command-line services measure both directions inside one process and
    // announce no stage of their own, so this is as specific as the page can be.
    case 'transfer': return t('Measuring download and upload…')
    default: return t('Starting…')
  }
}

/**
 * One internet speed test service per page.
 *
 * The three measure different paths — Ookla's nearest servers, the closest
 * Cloudflare edge, the Netflix Open Connect route a stream takes — so the tile
 * and the heading name the service, and the page never has to ask which one to
 * run. Ookla alone offers a server to pick; the other two are command-line
 * tools the daemon starts under the chosen way out, and either may not be
 * installed at all.
 */
function SpeedTest({ service }: { service: SpeedService }) {
  useLocale()
  const gateway = useGatewayConfig()
  const [exit, setExit] = useState('')
  const [serverID, setServerID] = useState('')
  const exits = gateway.data?.enabled ? (gateway.data.exits ?? []).filter((item) => item.via.kind !== 'blocked') : []
  const exitUnavailable = exit !== '' && !!gateway.data && !exits.some((item) => item.name === exit)
  const selectedExit = exit
  // Ookla ships inside the daemon; only the two command-line services can be
  // absent, and the daemon is the one that can look for them on PATH.
  const services = useQuery({
    queryKey: ['tools', 'speedtest', 'services'],
    queryFn: () => api.get<{ providers: SpeedProvider[] }>('/api/tools/speedtest/providers'),
    enabled: service !== 'ookla',
    staleTime: 60_000,
    retry: false,
  })
  const serviceInfo = services.data?.providers.find((item) => item.id === service)
  const serviceUnavailable = !!serviceInfo && !serviceInfo.available
  // The picker lists the servers this way out is offered. The API still takes a
  // search term; the page keeps the choice to one control.
  const servers = useQuery({
    queryKey: ['tools', 'speedtest', 'servers', selectedExit],
    queryFn: () => api.get<ServerList>(`/api/tools/speedtest/servers?${new URLSearchParams({ exit: selectedExit })}`),
    enabled: service === 'ookla',
    staleTime: 60_000,
    retry: false,
  })
  const options = servers.data?.servers ?? []
  const selectedServer = serverID

  const [result, setResult] = useState<SpeedResult | null>(null)
  const [running, setRunning] = useState(false)
  const [error, setError] = useState<TestError | null>(null)
  const [copied, setCopied] = useState(false)
  const [copyFailed, setCopyFailed] = useState(false)
  // The POST stays open until the measurement is done, so the stage it has
  // reached arrives on a second request. It polls rather than streams because
  // EventSource cannot send this API's Authorization header; only a snapshot
  // taken during *this* run is shown, so a previous run's numbers never pass
  // for a fresh one while the first poll is still in flight.
  const progress = useQuery({
    queryKey: ['tools', 'speedtest', 'progress'],
    queryFn: () => api.get<SpeedProgress>('/api/tools/speedtest/progress'),
    enabled: running,
    refetchInterval: 1_000,
    retry: false,
  })
  const live = running && progress.data?.running ? progress.data : undefined

  async function run() {
    if (exitUnavailable || serviceUnavailable) return
    setRunning(true)
    setResult(null)
    setError(null)
    setCopied(false)
    setCopyFailed(false)
    try {
      const query = new URLSearchParams({ exit: selectedExit, provider: service })
      if (service === 'ookla') query.set('server_id', selectedServer)
      setResult(await api.post<SpeedResult>(`/api/tools/speedtest?${query}`))
    } catch (err) {
      setError(explainFailure(err))
    } finally {
      setRunning(false)
    }
  }

  async function copyDiagnostics() {
    if (!error) return
    try {
      await navigator.clipboard.writeText(`Open Linux Router ${__APP_VERSION__}\nSpeed test: ${error.detail}`)
      setCopied(true)
      setCopyFailed(false)
    } catch {
      setCopyFailed(true)
    }
  }

  // The dials read the settled result when there is one, the rate the direction
  // being measured is reaching while the test runs, and nothing before that.
  const download = result ? result.download_mbps : live?.download_mbps ?? (live?.phase === 'download' ? live.live_mbps ?? null : null)
  const upload = result ? result.upload_mbps : live?.upload_mbps ?? (live?.phase === 'upload' ? live.live_mbps ?? null : null)
  const latency = result ? result.latency_ms : live?.latency_ms ?? null
  const jitter = result ? result.jitter_ms : null

  function serverLabel(value: unknown) {
    if (!value || value === 'auto') return t('Automatic (first working server)')
    const found = options.find((item) => item.id === value)
    return found ? `${found.sponsor} · ${found.location}` : String(value)
  }

  return <div className="space-y-8">
    {serviceUnavailable
      ? <Alert variant="destructive">
          <AlertTitle>{t('{name} is unavailable', { name: SPEED_SERVICE_NAMES[service] })}</AlertTitle>
          <AlertDescription className="space-y-3">
            {serviceInfo?.detail && <p className="break-words font-mono text-xs select-text">{serviceInfo.detail}</p>}
            <p>{t('Install that command on the router’s PATH, then reload this page.')}</p>
          </AlertDescription>
        </Alert>
      : <div className="panel-surface p-5 sm:p-8">
          <div className="mx-auto flex max-w-3xl flex-col items-center gap-6">
            <div className="flex flex-wrap items-center justify-center gap-2 sm:gap-3">
              <Select value={selectedExit || 'default'} onValueChange={(value) => { setExit(value === 'default' ? '' : value ?? ''); setServerID('') }} disabled={running || gateway.isPending}>
                <SelectTrigger aria-label={t('Way out')} className="w-48"><SelectValue>{(value) => (value === 'default' ? t('Router default route') : String(value))}</SelectValue></SelectTrigger>
                <SelectContent>
                  <SelectItem value="default">{t('Router default route')}</SelectItem>
                  {exits.map((item) => <SelectItem key={item.name} value={item.name}>{item.name}</SelectItem>)}
                </SelectContent>
              </Select>
              {service === 'ookla' && <Select value={selectedServer || 'auto'} onValueChange={(value) => setServerID(value === 'auto' ? '' : value ?? '')} disabled={running || servers.isPending}>
                <SelectTrigger aria-label={t('Test server')} className="w-72"><SelectValue>{(value) => serverLabel(value)}</SelectValue></SelectTrigger>
                <SelectContent>
                  <SelectItem value="auto">{t('Automatic (first working server)')}</SelectItem>
                  {options.map((item) => <SelectItem key={item.id} value={item.id}>{item.sponsor} · {item.location}</SelectItem>)}
                </SelectContent>
              </Select>}
            </div>
            {exitUnavailable && <p className="-mt-3 text-xs text-destructive">{t('This way out is no longer available. Choose another before testing.')}</p>}
            <Button size="lg" className="min-w-44 justify-center" onClick={run} disabled={running || gateway.isPending || exitUnavailable}>
              {running ? <LoaderCircle className="animate-spin" aria-hidden /> : <Activity aria-hidden />}
              {running ? t('Testing connection…') : result ? t('Run again') : t('Start speed test')}
            </Button>
            {(latency != null || jitter != null) && <div className="flex flex-wrap items-center justify-center gap-x-8 gap-y-2 text-sm text-muted-foreground">
              {latency != null && <span className="flex items-center gap-2">{t('Latency')}<MetricValue size="sm" value={latency.toFixed(1)} unit="ms" /></span>}
              {jitter != null && <span className="flex items-center gap-2">{t('Jitter')}<MetricValue size="sm" value={jitter.toFixed(1)} unit="ms" /></span>}
            </div>}
            <div className="flex flex-wrap items-start justify-center gap-10 sm:gap-16">
              <SpeedDial direction="download" label={t('Download')} value={download} active={running && (live?.phase === 'download' || live?.phase === 'transfer')} />
              <SpeedDial direction="upload" label={t('Upload')} value={upload} active={running && (live?.phase === 'upload' || live?.phase === 'transfer')} />
            </div>
            {result && <p className="text-center text-sm text-muted-foreground">{resultSummary(service, result)}</p>}
            {running && <span role="status" className="sr-only">{speedPhaseLabel(live?.phase)}</span>}
          </div>
        </div>}

      {error && (
        <Alert variant="destructive">
          <AlertTitle>{error.title}</AlertTitle>
          <AlertDescription className="space-y-3">
            <p>{error.guidance}</p>
            <details className="text-foreground">
              <summary className="cursor-pointer text-xs">{t('Technical details')}</summary>
              <p className="mt-2 break-words font-mono text-xs select-text">{error.detail}</p>
            </details>
            <div className="flex flex-wrap items-center gap-3">
              <Button variant="secondary" size="sm" onClick={copyDiagnostics}>
                <Copy aria-hidden />{copied ? t('Copied') : 'Copy diagnostics'}
              </Button>
              <a className="text-xs underline underline-offset-4" href="https://github.com/open-linux-router/open-linux-router/issues/new" target="_blank" rel="noreferrer">{t('Report an issue')}</a>
            </div>
            {copyFailed && <p className="text-xs">{t('Could not copy automatically. Select the technical details above to copy them.')}</p>}
            <p className="text-xs">Nothing is sent automatically. Paste the diagnostics into an issue only if you choose to report it.</p>
          </AlertDescription>
        </Alert>
      )}
  </div>
}

function LibreSpeed() {
  useLocale()
  const [granted, setGranted] = useState(false)
  const [grantError, setGrantError] = useState<string | null>(null)
  const asked = useRef(false)
  const grant = useMutation({
    mutationFn: () => api.post<void>('/api/tools/link-test/session'),
    onSuccess: () => setGranted(true),
    onError: error => setGrantError(error instanceof Error ? error.message : String(error)),
  })
  const requestSession = grant.mutate
  // The test page is its own document, so it takes its credential as a cookie
  // rather than as the header this page puts on every API call. Asking for one
  // on arrival is what makes the embedded test a single click: the operator
  // signed in here, so the grant is scoped to the test page and dies when the
  // browser session does.
  useEffect(() => {
    if (asked.current) return
    asked.current = true
    requestSession()
  }, [requestSession])

  // The tool page is the test page: no introduction and no second title above
  // it, so the embedded document is all the operator sees.
  return <section aria-label={t('Client to router')} className="panel-surface overflow-hidden">
    {/* The embedded page is a fixed-shape document: 960px holds all of it at
        desktop widths, so the frame needs no scrollbar of its own. Below sm
        the viewport is the honest bound and the frame scrolls instead. */}
    {granted
      ? <iframe title="LibreSpeed" src="/link-test/" className="h-[94dvh] w-full border-0 bg-[#0e0720] sm:h-[960px]" />
      : grantError
        ? <div className="p-4 sm:p-6"><Alert variant="destructive">
            <AlertTitle>{t('LibreSpeed is unavailable')}</AlertTitle>
            <AlertDescription className="space-y-3">
              <p className="break-words">{grantError}</p>
              <Button variant="secondary" size="sm" onClick={() => { setGrantError(null); requestSession() }}>{t('Try again')}</Button>
            </AlertDescription>
          </Alert></div>
        : <div role="status" className="flex h-40 items-center justify-center gap-2 text-sm text-muted-foreground"><LoaderCircle className="size-4 animate-spin" aria-hidden />{t('Loading the test page…')}</div>}
  </section>
}

function IPerf3() {
  useLocale()
  const lan = useQuery({
    queryKey: ['tools', 'lan-test'],
    queryFn: () => api.get<LANTest>('/api/tools/lan-test'),
    refetchInterval: 5_000,
    retry: false,
  })
  const [lanAddress, setLANAddress] = useState('')
  const [lanBusy, setLANBusy] = useState(false)
  const [lanError, setLANError] = useState<string | null>(null)
  const [lanCopied, setLANCopied] = useState<string | null>(null)
  const addresses = lan.data?.addresses ?? []
  const selectedLAN = addresses.includes(lanAddress) ? lanAddress : addresses[0] ?? ''
  const external = selectedLAN ? lan.data?.external?.[selectedLAN] : undefined
  const conflict = selectedLAN ? lan.data?.conflicts?.[selectedLAN] : undefined
  const testAddress = lan.data?.address || (external ? selectedLAN : '')

  async function changeLAN(start: boolean) {
    setLANBusy(true)
    setLANError(null)
    try {
      if (start) await api.post<LANTest>('/api/tools/lan-test', { address: selectedLAN })
      else await api.send<void>('DELETE', '/api/tools/lan-test')
      await lan.refetch()
    } catch (err) {
      setLANError(err instanceof Error ? err.message : String(err))
    } finally {
      setLANBusy(false)
    }
  }

  async function copyLAN(command: string) {
    try {
      await navigator.clipboard.writeText(command)
      setLANCopied(command)
    } catch {
      setLANError(t('Could not copy automatically. Select the command to copy it.'))
    }
  }

  return <div className="space-y-6">
    <section className="panel-surface p-4 sm:p-6" aria-labelledby="tool-title">
      <p className="max-w-2xl text-sm text-muted-foreground">{t('iperf3 measures sustained TCP streams with its own client on the other device. It is the honest choice for a wired link faster than a browser can measure, and it needs iperf3 installed on the router and on that device. It tests the link and the router’s own receive/send capacity, not LAN-to-WAN forwarding or NAT.')}</p>
      {lan.isError && <Alert variant="destructive" className="mt-4"><AlertTitle>{t('LAN addresses unavailable')}</AlertTitle><AlertDescription>{lan.error.message}</AlertDescription></Alert>}
      {lan.data && <div className="mt-5 space-y-4">
        <div className="max-w-sm space-y-1.5">
          <Label htmlFor="lan-address">{t('Router LAN address')}</Label>
          <Select value={lan.data.address || selectedLAN} onValueChange={value => setLANAddress(value ?? '')} disabled={!!lan.data.address || lanBusy || !addresses.length}>
            <SelectTrigger id="lan-address"><SelectValue placeholder={t('No configured LAN IPv4 address')} /></SelectTrigger>
            <SelectContent>{addresses.map(address => <SelectItem key={address} value={address}>{address}</SelectItem>)}</SelectContent>
          </Select>
        </div>
        {conflict && !lan.data.address && <Alert variant="destructive"><AlertTitle>{t('Port unavailable')}</AlertTitle><AlertDescription>{conflict}</AlertDescription></Alert>}
        {external && !lan.data.address && <p role="status" className="text-sm text-muted-foreground">{t('Existing iperf3 server:')} {external}. OLR did not start it and will not stop it.</p>}
        {(!external || lan.data.address) && <Button onClick={() => changeLAN(!lan.data?.address)} disabled={lanBusy || (!lan.data.address && (!selectedLAN || !!conflict))} variant={lan.data.address ? 'secondary' : 'primary'}>
          {lanBusy && <LoaderCircle className="animate-spin" aria-hidden />}
          {lan.data.address ? t('Stop server') : t('Start server')}
        </Button>}
        {testAddress && <div className="space-y-3 rounded-xl border bg-muted/30 p-4">
          <p className="text-sm font-medium">{lan.data.address
            ? t('OLR is listening on {address} until {time}', { address: `${testAddress}:${lan.data.port}`, time: new Date(lan.data.expires_at!).toLocaleTimeString() })
            : t('An existing server is running on {address}; its lifetime is managed outside OLR', { address: `${testAddress}:${lan.data.port}` })}</p>
          <p className="text-xs text-muted-foreground">{t('Anyone able to reach that address and port can use the server while it runs, and a configured firewall may need a temporary TCP 5201 opening. Run these on the other device’s terminal; the first sends data to OLR, the second makes OLR send data back.')}</p>
          {[
            `iperf3 -c ${testAddress} -P 4 -t 10`,
            `iperf3 -c ${testAddress} -P 4 -t 10 -R`,
          ].map((command, index) => <div key={command} className="space-y-1">
            <p className="text-xs font-medium">{index === 0 ? t('Client → router') : t('Router → device')}</p>
            <div className="flex flex-wrap items-center gap-2">
              <code className="min-w-0 flex-1 overflow-x-auto rounded-lg bg-background px-3 py-2 text-sm">{command}</code>
              <Button variant="secondary" size="sm" onClick={() => copyLAN(command)}><Copy aria-hidden />{lanCopied === command ? t('Copied') : t('Copy')}</Button>
            </div>
          </div>)}
        </div>}
      </div>}
      {lanError && <Alert variant="destructive" className="mt-4"><AlertTitle>{t('iperf3 is unavailable')}</AlertTitle><AlertDescription>{lanError}</AlertDescription></Alert>}
    </section>
  </div>
}

function NATTest() {
  useLocale()
  const [natResult, setNATResult] = useState<NATResult | null>(null)
  const [natRunning, setNATRunning] = useState(false)
  const [natError, setNATError] = useState<string | null>(null)

  async function runNAT() {
    setNATRunning(true)
    setNATResult(null)
    setNATError(null)
    try {
      setNATResult(await api.post<NATResult>('/api/tools/nat'))
    } catch (err) {
      setNATError(err instanceof Error ? err.message : String(err))
    } finally {
      setNATRunning(false)
    }
  }


  return <div className="space-y-6">
      <section className="panel-surface p-4 sm:p-6" aria-labelledby="tool-title">
        <p className="text-sm text-muted-foreground">{t('NatTypeTester probes public STUN servers from this router over its default route. It does not test a device behind the router or whether a game port is open.')}</p>
        <Button className="mt-5" onClick={runNAT} disabled={natRunning}>
          {natRunning && <LoaderCircle className="animate-spin" aria-hidden />}
          {natRunning ? t('Testing…') : t('Run NatTypeTester')}
        </Button>
        {natRunning && <p role="status" className="mt-4 text-sm text-muted-foreground">{t('Running both NatTypeTester probes. This may take up to 15 seconds.')}</p>}
        {natError && <Alert variant="destructive" className="mt-5"><AlertTitle>{t('NatTypeTester unavailable')}</AlertTitle><AlertDescription className="break-words">{natError}</AlertDescription></Alert>}
        {natResult && <NatTypeTesterResult result={natResult} />}
      </section>
  </div>
}

/**
 * NatTypeTester's answer: the RFC 3489 type first, then every fact the console
 * reports — the probe endpoints, the binding test, and the RFC 5780 mapping and
 * filtering. The disclosure keeps only the explanations.
 */
function NatTypeTesterResult({ result }: { result: NATResult }) {
  useLocale()
  return <div className="mt-6 space-y-3" aria-live="polite">
    <div className="rounded-lg bg-muted/50 p-4">
      <div className="flex items-baseline justify-between gap-3">
        <p className="text-sm text-muted-foreground">{t('NAT type')}</p>
        <span className="text-xs text-muted-foreground">RFC 3489</span>
      </div>
      <p className="mt-2 text-2xl leading-8 font-semibold">{natTypeLabel(result.nat_type)}</p>
      <p className="mt-1 max-w-xl text-sm leading-6 text-muted-foreground">{natTypeMeaning(result.nat_type)}</p>
    </div>

    <dl className="grid gap-x-8 gap-y-4 rounded-lg bg-muted/50 p-4 sm:grid-cols-2">
      <div>
        <dt className="text-xs text-muted-foreground">{t('Public address')}</dt>
        <dd className="mt-1 font-mono text-lg leading-7 font-semibold [overflow-wrap:anywhere]"><Address value={result.mapped || '—'} /></dd>
      </div>
      <div>
        <dt className="text-xs text-muted-foreground">{t('STUN server')}</dt>
        <dd className="mt-1 font-mono text-sm leading-6 [overflow-wrap:anywhere]"><Address value={result.server || '—'} /></dd>
      </div>
      <div>
        <dt className="text-xs text-muted-foreground">{t('Local endpoint')}</dt>
        <dd className="mt-1 font-mono text-sm leading-6 [overflow-wrap:anywhere]"><Address value={result.local || '—'} /></dd>
      </div>
      <div>
        <dt className="text-xs text-muted-foreground">{t('Binding test')}</dt>
        <dd className="mt-1 text-sm leading-6 font-medium">{bindingLabel(result.binding)}</dd>
      </div>
    </dl>

    <div className="grid gap-3 sm:grid-cols-2">
      <NATFact label={t('Mapping')} verdict={mappingVerdict(result.mapping)} observed={result.mapping} />
      <NATFact label={t('Filtering')} verdict={filteringVerdict(result.filtering)} observed={result.filtering} />
    </div>

    {result.error && <p className="text-xs leading-5 text-muted-foreground"><span className="font-medium text-foreground">{t('Incomplete result')}</span> · {result.error}</p>}
    <p className="text-xs leading-5 text-muted-foreground">{t('NatTypeTester reports one UDP flow from this router’s default route. It cannot prove that incoming connections reach this router.')}</p>
    <Disclosure summary={t('What these results mean')}>
      <ul className="space-y-2 pb-1 pl-5 text-xs leading-5 text-muted-foreground">
        <li className="list-disc">{t('NAT type is NatTypeTester’s RFC 3489 label: Open Internet (no NAT), Full Cone (one port any host can reach), Restricted Cone (only contacted hosts), Port Restricted Cone (only the exact contacted host and port), Symmetric (the port changes per destination).')}</li>
        <li className="list-disc">{t('Mapping: whether the public UDP port stays the same as the router contacts different destinations. One stable port is what a direct connection between two hosts needs.')}</li>
        <li className="list-disc">{t('Filtering: who may send back to that port. Any host is the most permissive; only the exact host and port already contacted is stricter.')}</li>
        <li className="list-disc">{t('The type and the mapping/filtering observations come from separate probes, so their public ports can differ.')}</li>
        <li className="list-disc">{t('This is not a console’s NAT type and cannot prove that a game or server accepts incoming connections.')}</li>
      </ul>
    </Disclosure>
  </div>
}

/** Breaks an address after the last colon, so a port never splits mid-number. */
function Address({ value }: { value: string }) {
  const colon = value.lastIndexOf(':')
  if (colon < 0) return <>{value}</>
  return <>{value.slice(0, colon)}:<wbr />{value.slice(colon + 1)}</>
}

function NATFact({ label, verdict, observed }: { label: string; verdict: string; observed: string }) {
  return <div className="rounded-lg bg-muted/50 p-4">
    <div className="flex items-baseline justify-between gap-3">
      <p className="text-sm text-muted-foreground">{label}</p>
      <span className="text-xs text-muted-foreground">RFC 5780</span>
    </div>
    <p className="mt-2 text-base leading-6 font-medium">{verdict}</p>
    {observed !== 'not tested' && <p className="mt-1 font-mono text-xs leading-4 text-muted-foreground">{behaviorLabel(observed)}</p>}
  </div>
}

function PingTest() {
  useLocale()
  const [target, setTarget] = useState('1.1.1.1')
  const [pingResult, setPingResult] = useState<PingResult | null>(null)
  const [pingRunning, setPingRunning] = useState(false)
  const [pingError, setPingError] = useState<string | null>(null)

  async function runPing(event: React.SubmitEvent<HTMLFormElement>) {
    event.preventDefault()
    setPingRunning(true)
    setPingResult(null)
    setPingError(null)
    try {
      setPingResult(await api.post<PingResult>('/api/tools/ping', { target: target.trim() }))
    } catch (err) {
      setPingError(err instanceof Error ? err.message : String(err))
    } finally {
      setPingRunning(false)
    }
  }


  return <div className="space-y-6">
      <section className="panel-surface p-4 sm:p-6" aria-labelledby="tool-title">
        <div className="flex items-start gap-4">
          <div className="space-y-1">
            <p className="text-sm text-muted-foreground">Send four ICMP probes from the router, not your browser, to check reachability and latency.</p>
          </div>
        </div>
        <form onSubmit={runPing} className="mt-6 flex flex-col gap-3 sm:flex-row sm:items-end">
          <label className="flex-1 space-y-2 text-sm font-medium" htmlFor="ping-target"> {t('Hostname or IP address')} <Input id="ping-target" value={target} onChange={event => setTarget(event.target.value)} placeholder="example.com or 192.168.1.1" maxLength={253} required disabled={pingRunning} />
          </label>
          <Button type="submit" disabled={pingRunning || !target.trim()}>
            {pingRunning && <LoaderCircle className="animate-spin" aria-hidden />}
            {pingRunning ? 'Pinging…' : 'Run ping'}
          </Button>
        </form>
        {pingRunning && <p role="status" className="mt-4 text-sm text-muted-foreground">{t('Sending four probes. This may take up to 10 seconds.')}</p>}
        {pingError && <Alert variant="destructive" className="mt-5"><AlertTitle>Ping unavailable</AlertTitle><AlertDescription className="break-words">{pingError}</AlertDescription></Alert>}
        {pingResult && (
          <div className="mt-6 space-y-4" aria-live="polite">
            <p className="text-sm text-muted-foreground">{pingResult.target} resolved to <span className="font-mono text-foreground">{pingResult.address}</span></p>
            <div className="grid gap-3 sm:grid-cols-3">
              <Metric surface="inset" icon={Radio} label={t('Packet loss')} value={pingResult.loss_percent} unit="%" detail={`${pingResult.received} of ${pingResult.sent} replies`} />
              <Metric surface="inset" icon={Activity} label={t('Average RTT')} value={pingResult.received ? pingResult.avg_ms : null} unit="ms" detail={pingResult.received ? `Min ${pingResult.min_ms.toFixed(1)} · Max ${pingResult.max_ms.toFixed(1)} ms` : 'No replies received'} />
              <div className="rounded-lg bg-muted/50 p-4 text-sm text-muted-foreground">
                {pingResult.received === 0 ? 'No replies. The destination may be unreachable or block ICMP.' : 'Replies received from the destination.'}
              </div>
            </div>
          </div>
        )}
      </section>
  </div>
}

interface EdgeResult {
  cdn: string
  pop?: string
  token?: string
  latency_ms: number
  error?: string
}

interface EdgeReport {
  edges: EdgeResult[]
}

/**
 * Which of each CDN's edge nodes this router reaches.
 *
 * The page states the scope in one line rather than in a paragraph: one request
 * per provider, the node that answered, and no claim about speed. A row that
 * failed keeps its provider name, because which CDN did not answer is the
 * finding.
 */
function CDNEdgeTest() {
  useLocale()
  const [report, setReport] = useState<EdgeReport | null>(null)
  const [running, setRunning] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function checkEdges() {
    setRunning(true)
    setError(null)
    try {
      setReport(await api.post<EdgeReport>('/api/tools/edge'))
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setRunning(false)
    }
  }

  return <div className="space-y-6">
      <section className="panel-surface p-4 sm:p-6" aria-labelledby="tool-title">
        <p className="text-sm text-muted-foreground">{t('Each provider is asked which of its edge nodes answered this router: one HTTPS request each, and no configuration change.')}</p>
        <Button className="mt-4" onClick={checkEdges} disabled={running}>
          {running ? <LoaderCircle className="animate-spin" aria-hidden /> : <Globe aria-hidden />}
          {running ? t('Checking edges…') : t('Check edges')}
        </Button>
        {running && <p role="status" className="mt-4 text-sm text-muted-foreground">{t('Waiting for three answers. This takes a few seconds.')}</p>}
        {error && <Alert variant="destructive" className="mt-5"><AlertTitle>{t('Could not check the edges')}</AlertTitle><AlertDescription className="break-words">{error}</AlertDescription></Alert>}
        {report && (
          <ul className="mt-6 divide-y divide-border rounded-lg bg-muted/40" aria-live="polite">
            {report.edges.map(edge => (
              <li key={edge.cdn} className="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1 px-4 py-3">
                <span className="text-sm font-medium">{edge.cdn}</span>
                {edge.error
                  ? <span className="text-sm text-destructive">{edge.error}</span>
                  : <span className="flex items-baseline gap-3">
                      <span className="font-mono text-lg font-semibold tracking-tight">{edge.pop || '—'}</span>
                      <span className="text-xs text-muted-foreground">{edge.token && edge.token !== edge.pop ? `${edge.token} · ` : ''}{edge.latency_ms < 1 ? '<1' : Math.round(edge.latency_ms)} ms</span>
                    </span>}
              </li>
            ))}
          </ul>
        )}
        {report && <p className="mt-3 text-xs text-muted-foreground">{t('A code names the node that answered, not the nearest one. A distant code can explain a slow site; this check does not measure speed.')}</p>}
      </section>
  </div>
}

interface Place {
  city?: string
  region?: string
  country?: string
  country_name?: string
  org?: string
}

interface DNSAnswer {
  source: string
  name: string
  ip?: string
  place?: Place
}

interface DNSLegResult {
  name: string
  server: string
  kind: string
  latency_ms: number
  answers: DNSAnswer[]
  error?: string
}

interface DNSExitReport {
  legs: DNSLegResult[]
}

/** Where a resolver appears to be, in the order a person reads it. */
function placeSummary(place?: Place): string | null {
  if (!place) return null
  const parts = [place.city, place.country_name || place.country].filter(Boolean)
  return parts.length > 0 ? parts.join(' · ') : null
}

/**
 * Where each resolver this router can name leaves from.
 *
 * The row names the resolver and the server it was asked at, so a reader can
 * tell "this router" from an exit's DNS. Two whoami names answer independently;
 * showing both keeps one provider's view from passing for the whole answer.
 */
function DNSExitTest() {
  useLocale()
  const [report, setReport] = useState<DNSExitReport | null>(null)
  const [running, setRunning] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function checkExit() {
    setRunning(true)
    setError(null)
    try {
      setReport(await api.post<DNSExitReport>('/api/tools/dns-exit'))
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setRunning(false)
    }
  }

  return <div className="space-y-6">
      <section className="panel-surface p-4 sm:p-6" aria-labelledby="tool-title">
        <p className="text-sm text-muted-foreground">{t('Each resolver this router can ask is asked where it leaves from, through two whoami names that report the address they see. No configuration changes.')}</p>
        <Button className="mt-4" onClick={checkExit} disabled={running}>
          {running ? <LoaderCircle className="animate-spin" aria-hidden /> : <Globe aria-hidden />}
          {running ? t('Asking resolvers…') : t('Check DNS exit')}
        </Button>
        {running && <p role="status" className="mt-4 text-sm text-muted-foreground">{t('Two queries per resolver. This takes a few seconds.')}</p>}
        {error && <Alert variant="destructive" className="mt-5"><AlertTitle>{t('Could not check the DNS exit')}</AlertTitle><AlertDescription className="break-words">{error}</AlertDescription></Alert>}
        {report?.legs.length === 0 && <p className="mt-6 text-sm text-muted-foreground">{t('DNS is off, so this router has no resolver to ask.')}</p>}
        {report && report.legs.length > 0 && (
          <ul className="mt-6 space-y-3" aria-live="polite">
            {report.legs.map(leg => (
              <li key={leg.name} className="rounded-lg bg-muted/40 p-4">
                <div className="flex flex-wrap items-baseline justify-between gap-2">
                  <span className="text-sm font-medium">{leg.name}</span>
                  <span className="text-xs text-muted-foreground">{leg.server} · {leg.latency_ms < 1 ? '<1' : Math.round(leg.latency_ms)} ms</span>
                </div>
                {leg.error
                  ? <p className="mt-2 text-sm text-destructive">{leg.error}</p>
                  : <ul className="mt-2 space-y-1">
                      {leg.answers.map(answer => {
                        const place = placeSummary(answer.place)
                        return <li key={answer.source} className="flex flex-wrap items-baseline gap-x-3 gap-y-1 text-sm">
                          <span className="font-mono">{answer.ip}</span>
                          {place && <span className="text-muted-foreground">{place}</span>}
                          <span className="text-xs text-muted-foreground">{t('reported by {source}', { source: answer.source })}</span>
                        </li>
                      })}
                    </ul>}
              </li>
            ))}
          </ul>
        )}
        {report && report.legs.length > 0 && <p className="mt-3 text-xs text-muted-foreground">{t('The address belongs to the resolver, not to this router. A resolver in another city can make a CDN hand you a distant node.')}</p>}
      </section>
  </div>
}

interface SplitEgress {
  ip: string
  place?: Place
  count: number
  countries: string[]
  servers: string[]
}

interface SplitFailure {
  server: string
  error: string
}

interface SplitReport {
  asked: number
  answered: number
  egresses: SplitEgress[]
  failures: SplitFailure[]
}

/**
 * How many ways out this network uses.
 *
 * The answer is the grouping, so the groups come first and the servers that
 * produced them second. The totals stay on screen because a run that answered
 * nine hundred of twelve hundred servers is a reading about nine hundred.
 */
function SplitCheckTest() {
  useLocale()
  const [report, setReport] = useState<SplitReport | null>(null)
  const [running, setRunning] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function checkEgress() {
    setRunning(true)
    setError(null)
    try {
      setReport(await api.post<SplitReport>('/api/tools/split-check'))
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setRunning(false)
    }
  }

  return <div className="space-y-6">
      <section className="panel-surface p-4 sm:p-6" aria-labelledby="tool-title">
        <p className="text-sm text-muted-foreground">{t('Each speed-test server reports the address it sees this router leave from. One address means every server took the same way out; several means the network sends different destinations out differently.')}</p>
        <Button className="mt-4" onClick={checkEgress} disabled={running}>
          {running ? <LoaderCircle className="animate-spin" aria-hidden /> : <Globe aria-hidden />}
          {running ? t('Checking egress…') : t('Check egress')}
        </Button>
        {running && <p role="status" className="mt-4 text-sm text-muted-foreground">{t('Asking up to {count} servers. A full run takes about a minute.', { count: 1200 })}</p>}
        {error && <Alert variant="destructive" className="mt-5"><AlertTitle>{t('Could not check the egress')}</AlertTitle><AlertDescription className="break-words">{error}</AlertDescription></Alert>}
        {report && (
          <>
            <p className="mt-6 text-sm text-muted-foreground">{t('Egress addresses: {count} · servers answered: {answered}/{asked}', { count: report.egresses.length, answered: report.answered, asked: report.asked })}</p>
            <ul className="mt-3 space-y-3" aria-live="polite">
              {report.egresses.map(egress => {
                const place = placeSummary(egress.place)
                return <li key={egress.ip} className="rounded-lg bg-muted/40 p-4">
                  <div className="flex flex-wrap items-baseline justify-between gap-2">
                    <span className="font-mono text-sm font-medium">{egress.ip}</span>
                    {place && <span className="text-sm text-muted-foreground">{place}</span>}
                  </div>
                  <p className="mt-1 text-xs text-muted-foreground">{t('{count} servers · {countries} countries', { count: egress.count, countries: egress.countries.length })}</p>
                  <p className="mt-1 text-xs text-muted-foreground">{egress.servers.join(' · ')}</p>
                </li>
              })}
            </ul>
            {report.failures.length > 0 && (
              <Disclosure summary={t('Servers that did not answer ({count})', { count: report.asked - report.answered })}>
                <ul className="space-y-1 text-xs text-muted-foreground">
                  {report.failures.map(failure => <li key={failure.server}>{failure.server}: <span className="break-words">{failure.error}</span></li>)}
                </ul>
              </Disclosure>
            )}
          </>
        )}
        {report && <p className="mt-3 text-xs text-muted-foreground">{t('The fleet is Ookla’s public speed-test server list. Several addresses is the finding; each address’s location is approximate (ip.sb).')}</p>}
      </section>
  </div>
}

function TracerouteTest() {
  useLocale()
  const [target, setTarget] = useState('1.1.1.1')
  const [trace, setTrace] = useState<TraceResult | null>(null)
  const [traceRunning, setTraceRunning] = useState(false)
  const [traceError, setTraceError] = useState<string | null>(null)

  async function runTraceroute(event: React.SubmitEvent<HTMLFormElement>) {
    event.preventDefault()
    setTraceRunning(true)
    setTrace(null)
    setTraceError(null)
    try {
      setTrace(await api.post<TraceResult>('/api/tools/traceroute', { target: target.trim() }))
    } catch (err) {
      setTraceError(err instanceof Error ? err.message : String(err))
    } finally {
      setTraceRunning(false)
    }
  }


  return <div className="space-y-6">
      <section className="panel-surface p-4 sm:p-6" aria-labelledby="tool-title">
        <div className="flex items-start gap-4">
          <div>
            <p className="mt-1 text-sm text-muted-foreground">{t('Follow the path from this router to a domain or IP address. Powered by NextTrace; sends network probes and may query its IP location service.')}</p>
          </div>
        </div>
        <form onSubmit={runTraceroute} className="mt-6 flex flex-col gap-3 sm:flex-row sm:items-end">
          <label className="flex-1 space-y-2 text-sm font-medium" htmlFor="trace-target">
            <span>{t('Destination')}</span>
            <Input id="trace-target" value={target} onChange={(event) => setTarget(event.target.value)} placeholder="example.com or 1.1.1.1" required maxLength={253} disabled={traceRunning} />
          </label>
          <Button type="submit" disabled={traceRunning || !target.trim()}>
            {traceRunning ? <LoaderCircle className="animate-spin" aria-hidden /> : <Route aria-hidden />}
            {traceRunning ? 'Tracing…' : 'Trace route'}
          </Button>
        </form>
        {traceRunning && <p role="status" className="mt-4 text-sm text-muted-foreground">Probing up to 20 hops. This may take up to 75 seconds.</p>}
        {traceError && <Alert variant="destructive" className="mt-5"><AlertTitle>{t('Traceroute unavailable')}</AlertTitle><AlertDescription className="break-words">{traceError}</AlertDescription></Alert>}
        {trace && <TraceView key={trace.map_url ?? trace.output} trace={trace} />}
      </section>
  </div>
}
function Metric({ icon: Icon, label, value, unit, detail, surface = 'panel' }: {
  icon: typeof Activity
  label: string
  value: number | null
  unit: string
  detail?: string
  surface?: 'panel' | 'inset'
}) {
  useLocale()
  return (
    <div className={surface === 'inset' ? 'rounded-lg bg-muted/50 p-4' : 'panel-surface p-4 sm:p-6'}>
      <div className="flex items-center gap-2 text-sm text-muted-foreground"><Icon className="size-4" aria-hidden />{label}</div>
      <MetricValue size="lg" value={value?.toFixed(1)} unit={unit} className="mt-4" />
      {detail && <p className="mt-2 text-xs text-muted-foreground">{detail}</p>}
    </div>
  )
}
