import { t, useLocale } from '@/lib/i18n'
import { AlertTriangle, ChevronRight } from 'lucide-react'
import { Link } from 'react-router'

import { SettingsList } from '@/components/layout/settings-list'
import { BlockerAlerts } from '@/components/layout/blockers'
import { StatusDetail, StatusStrip } from '@/components/layout/status-strip'
import { StuckSettings } from '@/components/layout/stuck-settings'
import { STUCK_SUMMARY } from '@/components/layout/stuck-summary'
import { Disclosure } from '@/components/ui/disclosure'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { ApplyOutcome, useDhcpEditor } from '@/features/dhcp/editor'
import { PlanDiff } from '@/features/dhcp/impact'
import { useDhcpLeases, useDhcpStatus } from '@/features/dhcp/queries'
import { useInterfaces } from '@/features/link/queries'
import type { DhcpStatus } from '@/lib/api-types'
import type { Tone } from '@/lib/state'
import type { DhcpConfig } from '@/lib/config-types'

/**
 * DHCP, on the page you land on.
 *
 * Is it handing out addresses, and how full are the ranges. The devices
 * themselves are deliberately not here — they are the body of the overview, and
 * showing the same list twice is what took the connected-devices card off this
 * page in the first place.
 */
export function DhcpContent() {
  useLocale()
  const { config, busy, change, applier, gate } = useDhcpEditor()
  const status = useDhcpStatus()
  const leases = useDhcpLeases()
  const interfaces = useInterfaces()

  if (!config) return gate
  const summary = describeStatus(config, status.data, status.error as Error | null)
  const active = leases.data?.leases.filter((lease) => lease.active).length
  const nothingAdopted = interfaces.isSuccess && !interfaces.data.interfaces.some((i) => i.adopted)

  return (
    <div className="space-y-4">
      <ApplyOutcome applier={applier} />
      {nothingAdopted && (
        <Alert><AlertTriangle /><AlertTitle>{t('Choose an interface first')}</AlertTitle><AlertDescription>{t('DHCP needs an adopted interface.')}<Link to="/gateway/interfaces" className="underline underline-offset-4">{t('Go to Interfaces')}</Link></AlertDescription></Alert>
      )}
      <div className="rounded-xl border bg-card p-5 space-y-4">
        <StatusStrip
          density="compact"
          tone={summary.tone}
          headline={summary.headline}
          detail={config.enabled && active !== undefined && summary !== STUCK_SUMMARY ? `${active} devices with active leases` : summary.detail}
          control={{
            id: 'dhcp-enabled-quick',
            get label() { return t('Hand out addresses') },
            checked: config.enabled,
            busy,
            onChange: (enabled) => change({ ...config, enabled }),
          }}
        />
        <div className="flex flex-wrap items-center justify-between gap-2 border-t pt-3 text-sm">
          <span className="text-muted-foreground">{config.pools?.length ?? 0} network{config.pools?.length === 1 ? '' : 's'} with address service</span>
          <Link to="/gateway/dhcp" className="inline-flex items-center gap-1 text-muted-foreground hover:text-foreground">{t('DHCP status and settings')}<ChevronRight className="size-4" aria-hidden /></Link>
        </div>
      </div>
      {config.enabled && status.data?.drifted && (
        <Alert><AlertTriangle /><AlertTitle>{t('DHCP settings differ from what is running')}</AlertTitle><AlertDescription><Link className="underline underline-offset-4" to="/gateway/dhcp">{t('Review and repair DHCP')}</Link></AlertDescription></Alert>
      )}
    </div>
  )
}

export function DhcpDetailsContent() {
  useLocale()
  const { config, busy, change, applier, gate } = useDhcpEditor()
  const status = useDhcpStatus()
  const leases = useDhcpLeases()
  const interfaces = useInterfaces()

  if (!config) return gate

  const connected = leases.data?.leases.filter((l) => l.active).length
  const pools = config.pools ?? []
  const nothingAdopted =
    interfaces.isSuccess && !interfaces.data.interfaces.some((i) => i.adopted)

  return (
    <div className="space-y-6">
      <ApplyOutcome applier={applier} />

      {/* The prerequisite, said before it is needed rather than as a refusal
          afterwards: olr will not serve anything on an interface nobody handed
          it (design.md §3.4), so on a fresh box every range added below is
          rejected until one switch on that page is on. */}
      {nothingAdopted && (
        <Alert>
          <AlertTriangle />
          <AlertTitle>{t('This router has not been given an interface yet')}</AlertTitle>
          <AlertDescription className="space-y-2">
            <p>
              Address ranges are refused until one of this machine's interfaces
              is handed over. Switching one on changes nothing by itself — no
              address is set and no service is started.
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
        connected={connected}
        busy={busy}
        onChange={change}
        onRepair={applier.repair}
      />

      <SettingsList
        section="/gateway/dhcp"
        rows={[
          {
            slug: 'ranges',
            value: pools.length
              ? pools.map((p) => p.network).join(', ')
              : 'None — nothing is handed out',
          },
          {
            slug: 'reservations',
            value: config.reservations?.length
              ? `${config.reservations.length} device${config.reservations.length === 1 ? '' : 's'}`
              : t('None'),
          },
          { slug: 'advanced', value: config.extra_dnsmasq_conf ? 'Customised' : undefined },
        ]}
      />
    </div>
  )
}

/* -------------------------------------------------------------------------- */

function StatusCard({
  config,
  status,
  error,
  connected,
  busy,
  onChange,
  onRepair,
}: {
  config: DhcpConfig
  status?: DhcpStatus
  error: Error | null
  connected?: number
  busy: boolean
  onChange: (next: DhcpConfig) => void
  /** Runs the pending work from what is already stored. */
  onRepair: () => void
}) {
  useLocale()
  const summary = describeStatus(config, status, error)

  // Only while it is on: see the note in routes/overview.tsx.
  const drifted = Boolean(config.enabled && status?.drifted && !status.drift_error)
  const changes = status?.drift?.changes ?? []

  return (
    <StatusStrip
      headline={summary.headline}
      detail={
        config.enabled && connected !== undefined && summary !== STUCK_SUMMARY
          ? `${connected} device${connected === 1 ? '' : 's'} connected`
          : summary.detail
      }
      tone={summary.tone}
      control={{
        id: 'dhcp-enabled',
        get label() { return t('Hand out addresses') },
        checked: config.enabled,
        busy,
        onChange: (enabled) => onChange({ ...config, enabled }),
      }}
      drifted={drifted}
      driftNote={
        status?.drift?.action === 'start' && !status.drift.changes.length
          ? 'The server is not running. Nothing else on this box is out of date.'
          : 'The running server is behind these settings.'
      }
      // Without this the row is a diagnosis with no cure. A server that is
      // enabled and simply not running drifts with an empty change list, and
      // then there is no setting left on the page to save.
      driftAction={{ get label() { return t('Apply now') }, busy, onClick: onRepair }}
      details={
        <>
          <StatusDetail term="Service">
            {status?.service
              ? `${status.service.unit} — ${status.service.state}${
                  status.service.sub_state ? ` (${status.service.sub_state})` : ''
                }`
              : (status?.service_error ?? 'unknown')}
          </StatusDetail>
          <StatusDetail term="Matches the running server">
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
      {/* The difference itself, not a claim that there is one. olr does not
          try to tell a deliberate hand-edit from a file left behind by a
          half-finished apply — the operator reading this knows which it was,
          and what they cannot get anywhere else is what actually differs. */}
      {drifted && changes.length > 0 && status?.drift && (
        <Disclosure
          summary={`What is different (${changes.length} file${changes.length === 1 ? '' : 's'})`}
        >
          <div className="pt-2">
            <PlanDiff plan={status.drift} />
          </div>
        </Disclosure>
      )}

      <StuckSettings
        error={config.enabled ? status?.drift_error : undefined}
        fix={{ to: '/gateway/dhcp/ranges', get label() { return t('Address ranges') } }}
      />

      {/* Usually the same dnsmasq the DNS page is complaining about: the
          distribution's unit takes UDP/67 and :53 together, so one
          `apt install dnsmasq` blocks both modules. Same component and the
          same words on both pages, so it reads as one problem rather than
          two. */}
      <BlockerAlerts module="dhcp" blockers={status?.blockers} />
    </StatusStrip>
  )
}

/**
 * "We could not tell" is a third answer and must not be flattened into
 * "stopped" (design.md §5.4) — hence the `Status unknown` branch, which is
 * reached when the daemon could not read the unit at all, as distinct from
 * reading it as inactive.
 */
function describeStatus(
  config: DhcpConfig,
  status: DhcpStatus | undefined,
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
      detail: 'Devices will not get an address from this router.',
      tone: 'off',
    }
  }
  if (!status.service) {
    return {
      headline: 'Status unknown',
      detail: status.service_error ?? 'The router could not read the service state.',
      tone: 'unknown',
    }
  }
  // Running, but on what it was given before: see StuckSettings. A green
  // "handing out addresses" here was true and was the whole problem — it was
  // handing them out on a network that no longer existed.
  if (status.service.active && status.drift_error) return STUCK_SUMMARY
  return status.service.active
    ? { headline: 'Handing out addresses', detail: 'Working normally.', tone: 'ok' }
    : {
        headline: 'Not running',
        detail: 'Turned on, but the service is stopped.',
        tone: 'fault',
      }
}
