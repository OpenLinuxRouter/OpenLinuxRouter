import { t, useLocale } from '@/lib/i18n'
import { AlertTriangle, ChevronRight } from 'lucide-react'
import { Link } from 'react-router'
import { BlockerAlerts } from '@/components/layout/blockers'
import { SettingsList } from '@/components/layout/settings-list'
import { StatusDetail, StatusStrip } from '@/components/layout/status-strip'
import { StuckSettings } from '@/components/layout/stuck-settings'
import { STUCK_SUMMARY } from '@/components/layout/stuck-summary'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Disclosure } from '@/components/ui/disclosure'
import { SwitchField } from '@/components/ui/editable-field'
import { ActivityCard } from '@/features/dns/activity'
import { PlanDiff } from '@/features/dns/impact'
import { ApplyOutcome, useDnsEditor } from '@/features/dns/editor'
import { useDnsQueries, useDnsStatus } from '@/features/dns/queries'
import { RELAY_UNIT, RESOLVER_UNIT, UnitLabel, serviceOf, unitLabel } from '@/features/dns/units'
import { useInterfaces } from '@/features/link/queries'
import type { DnsStatus } from '@/lib/api-types'
import type { Tone } from '@/lib/state'
import type { DnsConfig } from '@/lib/config-types'

/** A compact overview; the full query log and diagnostics live in DNS details. */
export function DnsContent() {
  useLocale()
  const { config, busy, change, applier, gate } = useDnsEditor()
  const status = useDnsStatus()
  const queries = useDnsQueries()
  const interfaces = useInterfaces()

  if (!config) return gate
  const summary = describeStatus(config, status.data, status.error as Error | null)
  const nothingAdopted = interfaces.isSuccess && !interfaces.data.interfaces.some((i) => i.adopted)
  const stats = queries.data?.stats

  return (
    <div className="space-y-4">
      <ApplyOutcome applier={applier} />
      {nothingAdopted && (
        <Alert><AlertTriangle /><AlertTitle>{t('Choose an interface first')}</AlertTitle><AlertDescription>{t('DNS needs an adopted interface.')}<Link to="/gateway/interfaces" className="underline underline-offset-4">{t('Go to Interfaces')}</Link></AlertDescription></Alert>
      )}
      <div className="rounded-xl border bg-card p-5 space-y-4">
        <StatusStrip
          density="compact"
          tone={summary.tone}
          headline={summary.headline}
          detail={summary.detail}
          control={{
            id: 'dns-enabled-quick',
            get label() { return t('Answer DNS for this network') },
            checked: config.enabled,
            busy,
            onChange: (enabled) => change({ ...config, enabled }),
          }}
        />
        <div className="grid gap-3 text-sm sm:grid-cols-2">
          <p><span className="text-muted-foreground">{t('Upstream')}</span><br />{summariseUpstream(config)}</p>
          <p><span className="text-muted-foreground">{t('Queries since restart')}</span><br />{stats ? `${stats.queries.toLocaleString()} looked up · ${stats.blocked.toLocaleString()} blocked` : queries.isError ? t('Unavailable') : t('Checking…')}</p>
        </div>
        <div className="flex flex-wrap items-center justify-between gap-3 border-t pt-3 text-sm">
          <SwitchField
            variant="inline"
            id="dns-query-log"
            label={t('Keep a query log')}
            hint={t('Records the names each device asks for. Kept in memory and dropped on restart.')}
            checked={config.query_log.enabled}
            busy={busy}
            onChange={(enabled) => change({ ...config, query_log: { ...config.query_log, enabled } })}
          />
          <Link to="/gateway/dns" className="inline-flex items-center gap-1 text-muted-foreground hover:text-foreground">{t('DNS activity and advanced settings')}<ChevronRight className="size-4" aria-hidden /></Link>
        </div>
        <SwitchField
          id="dns-summary-aaaa"
          label={t('Resolve IPv6 addresses (AAAA)')}
          hint={t('When off, AAAA queries return no address. IPv4 lookups continue normally.')}
          checked={!config.filter_aaaa}
          busy={busy}
          onChange={(enabled) => change({ ...config, filter_aaaa: !enabled })}
        />
      </div>
      {config.enabled && status.data?.drifted && (
        <Alert><AlertTriangle /><AlertTitle>{t('DNS settings differ from what is running')}</AlertTitle><AlertDescription><Link className="underline underline-offset-4" to="/gateway/dns">{t('Review and repair DNS')}</Link></AlertDescription></Alert>
      )}
    </div>
  )
}

export function DnsDetailsContent() {
  useLocale()
  const { config, busy, change, applier, gate } = useDnsEditor()
  const status = useDnsStatus()
  const interfaces = useInterfaces()

  if (!config) return gate

  // The one prerequisite this section cannot supply for itself. Turning DNS on
  // now chooses its own listen address (internal/dns WithDerivedListen), but it
  // chooses from the interfaces the operator has handed over — and with none,
  // there is nothing to choose. Said here, before the switch is touched, rather
  // than as the refusal that used to arrive after it.
  const nothingAdopted =
    interfaces.isSuccess && !interfaces.data.interfaces.some((i) => i.adopted)

  return (
    <div className="space-y-6">
      <ApplyOutcome applier={applier} />

      <SwitchField
        id="dns-details-aaaa"
        label={t('Resolve IPv6 addresses (AAAA)')}
        hint={t('When off, AAAA queries return no address. IPv4 lookups continue normally.')}
        checked={!config.filter_aaaa}
        busy={busy}
        onChange={(enabled) => change({ ...config, filter_aaaa: !enabled })}
      />

      {nothingAdopted && (
        <Alert>
          <AlertTriangle />
          <AlertTitle>{t('This router has not been given an interface yet')}</AlertTitle>
          <AlertDescription className="space-y-2">
            <p>
              DNS cannot answer anywhere until one of this machine's interfaces
              is handed over. Switching one on changes nothing by itself.
            </p>
            <Link
              to="/gateway/interfaces"
              className="inline-flex items-center gap-1 text-sm font-medium underline underline-offset-4"
            >{t('Choose an interface')}<ChevronRight className="size-3.5" aria-hidden />
            </Link>
          </AlertDescription>
        </Alert>
      )}

      <StatusCard
        config={config}
        status={status.data}
        error={status.error as Error | null}
        busy={busy}
        onChange={change}
        onRepair={applier.repair}
      />

      <ActivityCard config={config} busy={busy} onChange={change} />

      <SettingsList
        section="/gateway/dns"
        rows={[
          { slug: 'blocking', value: describeCount(config.policies?.length, 'rule') },
          { slug: 'names', value: describeCount(config.hosts?.length, 'name') },
          { slug: 'resolving', value: summariseUpstream(config) },
          // Blank is the default now, not a gap. The relay binds every address
          // the router holds, because an address written down here is a copy of
          // something the kernel owns and goes stale when the network changes.
          // "Nowhere yet" was right when this field had to be filled in; it
          // would now read as broken on a box that is working.
          { slug: 'listening', value: config.listen?.join(', ') || 'Every address' },
          { slug: 'enforcement', value: config.hijack.enabled ? t('On') : t('Off') },
          { slug: 'advanced', value: config.extra_unbound_conf ? 'Customised' : undefined },
        ]}
      />
    </div>
  )
}

function describeCount(n: number | undefined, noun: string): string {
  if (!n) return t('None')
  return `${n} ${noun}${n === 1 ? '' : 's'}`
}

/* -------------------------------------------------------------------------- */

function StatusCard({
  config,
  status,
  error,
  busy,
  onChange,
  onRepair,
}: {
  config: DnsConfig
  status?: DnsStatus
  error: Error | null
  busy: boolean
  onChange: (next: DnsConfig) => void
  /** Runs the pending work from what is already stored. */
  onRepair: () => void
}) {
  useLocale()
  const summary = describeStatus(config, status, error)

  // Reported on its own line rather than folded into the headline: a unit that
  // is running now and not enabled costs nothing until the next reboot, and
  // then costs the whole network its name resolution at once.
  //
  // "running now" is load-bearing and used to be missing from the predicate,
  // so a unit that was merely *stopped* and not enabled produced the sentence
  // "is running, but is not set to start at boot" directly under a headline
  // saying nothing was serving DNS. A first install shows exactly that: no unit
  // is enabled or active until an apply succeeds, so the page's first
  // impression was two lines contradicting each other.
  const notAtBoot = (status?.services ?? []).filter(
    (s) => s.status && !s.status.enabled && s.status.active,
  )

  // Its own alert rather than a branch inside the one above, which is where it
  // used to live and where its heading was wrong. A unit that is absent cannot
  // start at all — "will not come back after a reboot" understates it by a
  // whole outage, and the fix is a reinstall rather than a systemctl enable.
  const notInstalled = (status?.services ?? []).filter((s) => s.status?.installed === false)

  // Only while it is on: see the note in routes/overview.tsx. A module that is
  // off has nothing running to be behind.
  const drifted = Boolean(config.enabled && status?.drifted && !status.drift_error)
  const changes = status?.drift?.changes ?? []

  return (
    <StatusStrip
      headline={summary.headline}
      detail={summary.detail}
      tone={summary.tone}
      control={{
        id: 'dns-enabled',
        get label() { return t('Answer DNS for this network') },
        checked: config.enabled,
        busy,
        onChange: (enabled) => onChange({ ...config, enabled }),
      }}
      drifted={drifted}
      driftNote={driftNote(status)}
      // The button this row went without. Every other apply on this screen is a
      // side effect of an edit, which leaves no way at all to run a plan that
      // is one unstarted unit and an empty change list: the switch an operator
      // would reach for is already where they want it, and the only direction
      // it moves is the one that takes DNS away from the whole network.
      driftAction={{ get label() { return t('Apply now') }, busy, onClick: onRepair }}
      details={
        <>
          {/* Both backends, separately. Averaging them would hide the thing
              worth knowing: the resolver behind dying and the thing owning
              :53 dying are different faults with different fixes, and only
              one of them is ours. */}
          {(status?.services ?? []).map((s) => (
            <StatusDetail key={s.unit} term={s.unit}>
              {s.status
                ? `${s.status.state}${s.status.sub_state ? ` (${s.status.sub_state})` : ''}`
                : (s.error ?? 'unknown')}
            </StatusDetail>
          ))}
          <StatusDetail term="Resolving by">{describeUpstream(config)}</StatusDetail>
          <StatusDetail term="Matches what is running">
            {status?.drift_error
              ? `unknown — ${status.drift_error}`
              : status?.drifted
                ? 'no'
                : 'yes'}
          </StatusDetail>
          <StatusDetail term="Read at">
            {status ? new Date(status.as_of).toLocaleTimeString() : '—'}
          </StatusDetail>
          <p className="pt-1">
            Read from the system on every request and never cached, so this is
            what is true right now rather than what was last written.
          </p>
        </>
      }
    >
      {/* The difference itself, not a claim that there is one.
          olr cannot tell a deliberate hand-edit from a file that was corrupted
          or left behind by a half-finished apply, and it should not try — the
          operator reading this knows which it was, and the only thing they
          cannot get anywhere else is what actually differs. Behind a disclosure
          because on a healthy box there is nothing here, and the button above
          is the answer for everyone who does not want to read a diff. */}
      {drifted && changes.length > 0 && status?.drift && (
        <Disclosure summary={`What is different (${describeCount(changes.length, 'file')})`}>
          <div className="pt-2">
            <PlanDiff plan={status.drift} />
          </div>
        </Disclosure>
      )}

      <StuckSettings
        error={config.enabled ? status?.drift_error : undefined}
        fix={
          status?.drift_error?.startsWith('listen')
            ? { to: '/gateway/dns/listening', get label() { return t('Listening') } }
            : undefined
        }
      />

      <BlockerAlerts module="dns" blockers={status?.blockers} />

      {notInstalled.length > 0 && (
        <Alert variant="destructive">
          <AlertTriangle />
          <AlertTitle>{t('DNS cannot start — a unit is missing')}</AlertTitle>
          <AlertDescription>
            {notInstalled.map((s) => (
              <p key={s.unit}>
                {UnitLabel(s.unit)} is not installed on this box. The unit ships inside
                olr and is written by `olr enable`, so reinstall the package or run
                `sudo olr enable`.
              </p>
            ))}
          </AlertDescription>
        </Alert>
      )}

      {/* Tinted rather than given an Alert variant of its own: the shared
          component only ships default and destructive, and this is not
          destructive — nothing is wrong yet, which is exactly the problem. */}
      {notAtBoot.length > 0 && (
        <Alert className="border-warning/40 bg-warning/10 text-warning-foreground">
          <AlertTriangle />
          <AlertTitle>{t('DNS will not come back after a reboot')}</AlertTitle>
          <AlertDescription className="text-warning-foreground/90">
            {notAtBoot.map((s) => (
              <p key={s.unit}>
                {UnitLabel(s.unit)} is running, but is not set to start at boot.
              </p>
            ))}
          </AlertDescription>
        </Alert>
      )}
    </StatusStrip>
  )
}

/**
 * What the drift row says, when the pending work is worth naming.
 *
 * The generic sentence — what is running is behind these settings — describes
 * the case the rest of the UI assumes: a rendered file somebody edited, which
 * any re-save puts back. It says nothing useful about the case where every file
 * is right and a backend was simply never started, and that is the one an
 * operator meets on a box configured after its last boot. Worth naming, because
 * it reads like a detail and means no device on this network can look up a
 * name.
 */
function driftNote(status: DnsStatus | undefined): string | undefined {
  const drift = status?.drift
  if (!drift || drift.changes.length > 0) return undefined

  const idle = drift.services.filter((s) => s.action === 'start').map((s) => unitLabel(s.unit))
  if (idle.length === 0) return undefined

  const names =
    idle.length === 1 ? idle[0] : `${idle.slice(0, -1).join(', ')} and ${idle[idle.length - 1]}`
  return `${names.charAt(0).toUpperCase()}${names.slice(1)} ${
    idle.length === 1 ? 'is' : 'are'
  } not running. Nothing else on this box is out of date.`
}

/**
 * The headline.
 *
 * Two daemons make this harder than dhcp's version, and the extra cases are the
 * point rather than an inconvenience. "Clients can reach us but we cannot look
 * anything up" and "devices cannot reach us at all" both present to a human as
 * the internet being broken, and they have completely different fixes.
 *
 * "We could not tell" stays a third answer throughout (design.md §5.4): a box
 * with no system bus reports unknown, and flattening that into "stopped" would
 * put a red dot on a working router.
 */
function describeStatus(
  config: DnsConfig,
  status: DnsStatus | undefined,
  error: Error | null,
): { headline: string; detail: string; tone: Tone } {
  if (error) {
    return { headline: 'Cannot reach the router', detail: error.message, tone: 'fault' }
  }
  if (!status) {
    return {
      headline: 'Checking…',
      detail: 'Reading the current state.',
      tone: 'unknown',
    }
  }
  if (!config.enabled) {
    return {
      headline: 'Off',
      detail: 'Clients cannot look up names through this router.',
      tone: 'off',
    }
  }

  const relay = serviceOf(status, RELAY_UNIT)
  const resolver = serviceOf(status, RESOLVER_UNIT)

  if (!relay?.status && !resolver?.status) {
    return {
      headline: 'Status unknown',
      detail: relay?.error ?? resolver?.error ?? 'The router could not read the service state.',
      tone: 'unknown',
    }
  }
  if (relay?.status && !relay.status.active) {
    return {
      headline: 'Not answering',
      detail: 'Turned on, but nothing is serving DNS. Clients cannot look up names.',
      tone: 'fault',
    }
  }
  if (resolver?.status && !resolver.status.active) {
    return {
      headline: 'Answering, but nothing is resolving',
      detail:
        'Clients reach this router, but it cannot look anything up behind the scenes. Lookups will fail.',
      tone: 'fault',
    }
  }
  if (status.drift_error) return STUCK_SUMMARY
  return {
    headline: 'Answering queries',
    detail: describeUpstream(config),
    tone: 'ok',
  }
}

/**
 * The same fact as describeUpstream, in the width a list row has.
 *
 * Separate rather than shortening the sentence for both: the strip is prose
 * under a headline and reads as one ("Answering queries — forwarding to
 * 1.1.1.1"), while the row is a value beside a label and a full stop in it
 * would look like a mistake.
 */
function summariseUpstream(config: DnsConfig): string {
  const upstream = config.upstream
  if ((upstream.mode || 'recurse') !== 'forward') return 'From the root servers'
  const servers = upstream.servers?.length ? upstream.servers.join(', ') : 'Nowhere yet'
  return upstream.tls ? `${servers}, encrypted` : servers
}

function describeUpstream(config: DnsConfig): string {
  const upstream = config.upstream
  if ((upstream.mode || 'recurse') === 'forward') {
    const servers = upstream.servers?.length ? upstream.servers.join(', ') : 'nowhere yet'
    return upstream.tls
      ? `Forwarding to ${servers} over an encrypted connection.`
      : `Forwarding to ${servers}.`
  }
  return 'Looking names up from the root servers itself.'
}
