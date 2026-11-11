import { t, useLocale } from '@/lib/i18n'
import { AlertTriangle, KeyRound, Plus } from 'lucide-react'
import { useState } from 'react'

import { BlockerAlerts } from '@/components/layout/blockers'
import { SubPage } from '@/components/layout/sub-page'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { StatusPill } from '@/components/ui/status'
import { state } from '@/lib/state'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Disclosure } from '@/components/ui/disclosure'
import { List, ListEmpty, ListRow } from '@/components/ui/list'
import { Skeleton } from '@/components/ui/skeleton'
import { Switch } from '@/components/ui/switch'
import { DeviceIcon } from '@/features/clients/client-icon'
import { ConfirmDialog } from '@/features/remote/apply-dialog'
import { ClientConfigDialog } from '@/features/remote/client-config'
import { PeerBadge } from '@/features/remote/peer-badge'
import { PeerDialog } from '@/features/remote/peer-dialog'
import { SocksCard } from '@/features/remote/socks-card'
import {
  remoteChange,
  useRemoteConfig,
  useRemoteStatus,
  useReapplyRemote,
  useSocksStatus,
  type RemoteChangeRequest,
} from '@/features/remote/queries'
import { SettingsDialog } from '@/features/remote/settings-dialog'
import { useRemoteApply } from '@/features/remote/use-apply'
import { WayMark } from '@/features/remote/way-mark'
import type { RemotePeer, RemoteStatus } from '@/lib/api-types'

export function RemotePage({ way }: { way: 'wireguard' | 'socks-server' }) {
  useLocale()
  const slug = way
  const config = useRemoteConfig()
  const status = useRemoteStatus()
  const socks = useSocksStatus()
  const applier = useRemoteApply()
  const reapply = useReapplyRemote()

  const [adding, setAdding] = useState(false)
  const [settings, setSettings] = useState(false)

  if (config.isPending) return <SubPage section="/access" slug={slug}><PageSkeleton /></SubPage>
  if (config.isError) {
    return (
      <SubPage section="/access" slug={slug}><Alert variant="destructive">
        <AlertTriangle />
        <AlertTitle>{t('Could not load remote access')}</AlertTitle>
        <AlertDescription>{(config.error as Error).message}</AlertDescription>
      </Alert></SubPage>
    )
  }

  const wg = config.data.wireguard
  const peers = status.data?.peers ?? []
  const change = (c: RemoteChangeRequest) => applier.submit(c)
  const selectedStatus = way === 'socks-server' ? socks : status

  return (
    <SubPage section="/access" slug={slug}>
      {selectedStatus.isError && <Alert variant="destructive"><AlertTriangle /><AlertTitle>{t('Status unavailable')}</AlertTitle><AlertDescription>{selectedStatus.error?.message}</AlertDescription></Alert>}
      {selectedStatus.isPending && <p className="text-sm text-muted-foreground">{t('Loading status…')}</p>}
      {applier.failure && (
        <Alert variant="destructive" role="alert">
          <AlertTriangle />
          <AlertTitle>{t('Only part of the change went through')}</AlertTitle>
          <AlertDescription className="space-y-3">
            <p> {t('The steps that finished have already taken effect and will not be undone. Fixing the cause and applying again is safe — it picks up where this left off.')} </p>
            <Disclosure summary={t('Which steps ran')}>
              <ul className="space-y-1 font-mono text-xs">
                {applier.failure.steps?.map((s, i) => (
                  <li key={i}>
                    {s.done ? 'done   ' : 'failed '} {s.description}
                    {s.error ? ` — ${s.error}` : ''}
                  </li>
                ))}
              </ul>
            </Disclosure>
            <Button size="sm" variant="secondary" onClick={applier.dismissFailure}> {t('Dismiss')} </Button>
          </AlertDescription>
        </Alert>
      )}

      {way === 'wireguard' && <>
      {/* Ordered by what has to be true first. Without wireguard-tools nothing
          below it can work, so the blocker comes before the switch that would
          fail against it. */}
      <BlockerAlerts blockers={status.data?.blockers} module="remote" />

      {status.data?.interface_foreign && (
        <Alert variant="destructive">
          <AlertTriangle />
          <AlertTitle>{status.data.interface} belongs to something else</AlertTitle>
          <AlertDescription>
            An interface with that name already exists on this box and is not WireGuard&rsquo;s, so
            olr will not touch it. Give the tunnel a different name, or remove the other one.
          </AlertDescription>
        </Alert>
      )}

      {status.data?.drifted && !status.data.interface_foreign && (
        <Alert>
          <AlertTriangle />
          <AlertTitle>{t('The router is not doing what these settings say')}</AlertTitle>
          <AlertDescription className="space-y-3">
            <p>
              The tunnel is not set up the way it is configured — after a reboot, or because
              something removed it. Putting it back does not disturb anyone already connected.
            </p>
            <Button
              size="sm"
              variant="secondary"
              disabled={reapply.isPending}
              onClick={() => reapply.mutate()}
            > {t('Put it back')} </Button>
          </AlertDescription>
        </Alert>
      )}

      <ConnectionCard
        status={status.data}
        enabled={wg.enabled}
        busy={applier.busy}
        onChange={(enabled) => change(remoteChange.settings({ enabled }))}
        onEdit={() => setSettings(true)}
      />

      <Card>
        <CardHeader>
          <CardTitle>{t('WireGuard devices')}</CardTitle>
          <CardDescription> {t('Each device has its own key.')} </CardDescription>
          <CardAction>
            <Button size="sm" onClick={() => setAdding(true)}>
              <Plus /> {t('Add')} </Button>
          </CardAction>
        </CardHeader>
        <CardContent>
          <PeerList peers={peers} />
        </CardContent>
      </Card>

      </>}
      {way === 'socks-server' && !socks.isPending && !socks.isError && <SocksCard status={socks.data} applier={applier} />}

      <PeerDialog
        open={adding}
        onOpenChange={setAdding}
        subnet={status.data?.subnet}
        onSubmit={(peer) =>
          change(
            remoteChange.savePeer(peer.name, {
              routes: peer.routes,
              display_name: peer.display_name || undefined,
              public_key: peer.public_key,
            }),
          )
        }
      />
      <SettingsDialog
        open={settings}
        onOpenChange={setSettings}
        initial={config.data}
        onSubmit={async (endpoint, wireguard) => {
          // Two owners, so up to two requests — and the endpoint first,
          // because it is the one the daemon may refuse. Sending the tunnel's
          // fields first would leave them applied against an endpoint the
          // operator then declined to change.
          if (endpoint !== (config.data.endpoint ?? '')) {
            if (!(await change(remoteChange.endpoint(endpoint)))) return
          }
          if (Object.keys(wireguard).length > 0) {
            await change(remoteChange.settings(wireguard))
          }
        }}
      />

      {applier.issued && (
        <ClientConfigDialog result={applier.issued} onClose={applier.dismissIssued} />
      )}
      <ConfirmDialog applier={applier} />
    </SubPage>
  )
}

/**
 * The tunnel, in one card: whether it is running, where a device dials, the key
 * it checks, and the switch that closes all of them at once.
 *
 * It was two cards — a status strip above a settings card — and neither could
 * stand alone. The strip counted connected devices directly above a list that
 * already names every one of them and says when each was last heard, and the
 * card below it said where to dial without saying whether anything was running.
 * What is left here is what the list below cannot say: the address to point a
 * device at, the key it will see there, and the off state, which is the one
 * thing about it that is invisible from a list of saved devices.
 *
 * The public key is on screen rather than hidden behind an "advanced" anything.
 * It is not a secret — it is in every configuration this router has ever
 * handed out — and it is the one value somebody completing a file by hand, or
 * comparing against what their phone shows, actually needs.
 */
function ConnectionCard({
  status,
  enabled,
  busy,
  onChange,
  onEdit,
}: {
  status?: RemoteStatus
  enabled: boolean
  busy: boolean
  onChange: (enabled: boolean) => void
  onEdit: () => void
}) {
  useLocale()
  return (
    <Card>
      <CardHeader>
        <CardTitle id="remote-connection-title" className="flex min-w-0 flex-wrap items-center gap-2">
          <WayMark way="wireguard" fallback={KeyRound} className="size-4" />
          {t('Connection settings')}
          <TunnelState status={status} enabled={enabled} />
          {status && !status.endpoint && <StatusPill tone="fault">{t('Not set')}</StatusPill>}
        </CardTitle>
        <CardAction className="flex items-center gap-2">
          <Switch
            id="remote-enabled"
            aria-labelledby="remote-connection-title"
            checked={enabled}
            disabled={busy}
            onCheckedChange={onChange}
          />
        </CardAction>
      </CardHeader>
      <CardContent className="space-y-1 text-sm text-muted-foreground">
        {!enabled && (
          <p> {t('WireGuard is off. The devices below are saved, but none of them can connect.')} </p>
        )}
        {status?.endpoint ? (
          <p> {t('Clients connect to')} <span className="font-mono">{status.endpoint}</span> on UDP port{' '}
            {status.listen_port} and get addresses in{' '}
            <span className="font-mono">{status.subnet}</span>.
          </p>
        ) : (
          <p> {t('Set a public address before generating device configurations.')} </p>
        )}
        {status?.public_key && (
          <p className="truncate text-xs"> {t('Public key')} <span className="font-mono">{status.public_key}</span>
          </p>
        )}
        {/* The card's one control is the switch, so the way into its settings
            is a line of the card rather than a second button beside it. */}
        <div className="pt-2">
          <Button size="sm" variant="secondary" onClick={onEdit}> {t('Connection settings')} </Button>
        </div>
      </CardContent>
    </Card>
  )
}

/**
 * Switched on and running are different claims here in a way they are not on a
 * module that renders a file: this one's configuration lives in the kernel, so a
 * reboot leaves the settings intact and the tunnel gone. Painting that green
 * would be the screen agreeing with itself rather than with the box, so the word
 * follows the kernel and the switch keeps following the intent — the drift alert
 * above is what explains the two disagreeing.
 */
function TunnelState({ status, enabled }: { status?: RemoteStatus; enabled: boolean }) {
  useLocale()
  const health = !enabled ? state('Off')
    : !status || !status.kernel_known ? state('Unknown')
      : status.interface_present && status.interface_up && !status.interface_foreign ? state('Running')
        : state('Not running')
  return <StatusPill tone={health.tone}>{health.label}</StatusPill>
}

function PeerList({ peers }: { peers: RemotePeer[] }) {
  useLocale()
  if (peers.length === 0) {
    return (
      <ListEmpty>
        No devices yet. Adding one gives you a WireGuard configuration to scan on a phone or import
        on a laptop.
      </ListEmpty>
    )
  }

  return (
    <List>
      {peers.map((p) => (
        <ListRow
          key={p.name}
          // The row opens what the tunnel knows about this device; changing it
          // is the pencil, which stays outside the link.
          to={`/clients/wireguard/${encodeURIComponent(p.name)}`}
          leading={<DeviceIcon icon={p.icon} category={p.category ?? ''} online={p.online} size="sm" />}
          title={p.display_name || p.name}
          subtitle={describePeer(p)}
          trailing={<PeerBadge peer={p} />}
        />
      ))}
    </List>
  )
}

function describePeer(p: RemotePeer): string {
  const sends =
    p.routes === 'everything' ? 'sends everything home' : 'reaches your network'
  const where = `${p.address ?? '—'} · ${sends}`
  // The name stays on the row when a label hides it: it is what the commands
  // and the address bar take, and the row is where somebody reads it off.
  return p.display_name ? `${p.name} · ${where}` : where
}

function PageSkeleton() {
  useLocale()
  return (
    <div className="space-y-6">
      <Skeleton className="h-8 w-40" />
      <Skeleton className="h-28 w-full" />
      <Skeleton className="h-28 w-full" />
      <Skeleton className="h-56 w-full" />
    </div>
  )
}
