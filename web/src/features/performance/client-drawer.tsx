import { useState } from 'react'

import { Button } from '@/components/ui/button'
import { Heading } from '@/components/ui/heading'
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import { t, useLocale } from '@/lib/i18n'
import { formatRate } from '@/lib/utils'

import { clientAddressLabel, clientDirections, clientName, clientThroughput, isCensusMeasured } from './connections'
import {
  apiCapReached,
  clientFlows,
  flowDirections,
  flowEndpoints,
  flowPeer,
  flowShare,
  FLOW_ROW_LIMIT,
  peerLabel,
  sortFlows,
  usableFlows,
  visibleFlows,
} from './flows'
import { when } from './live'
import { Readout } from './parts'
import { failureReason, FLOW_API_LIMIT, queryFailure, useGatewayFlows, type ClientConnection } from './queries'
import { formatPercent } from './samples'

/**
 * One client's top flows, opened from its table row.
 *
 * This answers "this client figure is the sum of these flows" — the
 * composition, not a hypothesis about why the figure is what it is. The client
 * total is read from the connections endpoint and stays visible while the flow
 * endpoint loads, fails, or is absent on this daemon.
 */
export function ClientFlows({ client, windowSeconds, asOf, onClose }: {
  client?: ClientConnection
  windowSeconds?: number
  asOf?: string
  onClose: () => void
}) {
  const locale = useLocale()
  const identity = client?.mac ?? client?.ip ?? client?.name ?? ''
  // A different client is a different list; a previous drawer's "show more"
  // must not carry over to it. The identity is part of the state, so a new
  // client reads as collapsed without an effect that renders twice.
  const [expandedState, setExpandedState] = useState({ identity, expanded: false })
  const expanded = expandedState.identity === identity && expandedState.expanded
  const setExpanded = (value: boolean) => setExpandedState({ identity, expanded: value })
  const query = useGatewayFlows(FLOW_API_LIMIT, client != null)

  if (!client) return null

  const name = clientName(client) ?? t('Unnamed client')
  const address = clientAddressLabel(client)
  const { down, up } = clientDirections(client)
  const total = clientThroughput(client)
  const measuredTime = asOf ? when(asOf, locale) : undefined
  const windowNote = windowSeconds != null
    ? measuredTime
      ? t('Measured over the last {count} seconds, as of {time}.', { count: windowSeconds, time: measuredTime })
      : t('Measured over the last {count} seconds.', { count: windowSeconds })
    : measuredTime
      ? t('Measured as of {time}.', { time: measuredTime })
      : t('Measured by the gateway.')

  const failure = queryFailure(query.error)
  const reading = query.isError ? undefined : usableFlows(query.data)
  const measured = reading != null && isCensusMeasured(reading.state)
  const matched = measured && reading ? sortFlows(clientFlows(reading.flows, client)) : []
  const { visible, hidden } = visibleFlows(matched, expanded)
  const responseWindow = reading?.windowSeconds
  const shareNote = responseWindow != null
    ? t('Rates are measured over the last {count} seconds; shares are derived from the client total.', { count: responseWindow })
    : t('Rates are measured by the gateway; shares are derived from the client total.')

  const totalValue = down == null && up == null
    ? undefined
    : <span className="inline-flex flex-wrap items-baseline gap-x-3">
        <span className="text-receive">↓ {down == null ? '—' : formatRate(down / 8)}</span>
        <span className="text-transmit">↑ {up == null ? '—' : formatRate(up / 8)}</span>
      </span>

  let unavailable: string | undefined
  if (query.isPending) unavailable = t('Reading flows…')
  else if (failure === 'missing') unavailable = t('Per-flow detail is unavailable: this daemon does not serve /api/gateway/flows. The client total above is unaffected.')
  else if (query.isError) {
    const reason = failureReason(query.error)
    unavailable = reason
      ? t('The flow list could not be read: {reason}', { reason })
      : t('The flow list could not be read. The client total above is unaffected.')
  }
  else if (!reading) unavailable = t('The flow list could not be read. The client total above is unaffected.')
  else if (reading.state === 'warming') unavailable = t('The flow list is warming up. A complete window is not available yet.')
  else if (reading.state === 'disabled') {
    unavailable = reading.error
      ? t('The flow list is disabled: {reason}', { reason: reading.error })
      : t('The flow list is disabled.')
  }
  else if (reading.state === 'unavailable') {
    unavailable = reading.error
      ? t('The flow list could not be read: {reason}', { reason: reading.error })
      : t('The flow list could not be read. The client total above is unaffected.')
  }
  else if (reading.flows.length === 0) unavailable = t('The gateway reported no flows in this window.')
  else if (matched.length === 0) unavailable = t('No flow for this client is in the returned top-flow list.')

  return <Sheet open onOpenChange={open => { if (!open) onClose() }}>
    <SheetContent side="right" className="gap-0 p-0 data-[side=right]:w-full data-[side=right]:sm:max-w-md">
      <SheetHeader className="border-b p-4 pr-12">
        <SheetTitle>{t('Flows for {name}', { name })}</SheetTitle>
        <SheetDescription className="font-mono text-xs">
          {address ?? t('No address reported')}
        </SheetDescription>
      </SheetHeader>

      <div className="min-h-0 flex-1 space-y-3 overflow-y-auto p-4">
        <Readout
          label={t('Client total')}
          value={totalValue}
          provenance={total == null ? undefined : t('Measured')}
          note={windowNote}
        />

        {unavailable
          ? <p className="text-xs leading-5 text-muted-foreground">{unavailable}</p>
          : <>
              <div className="flex flex-wrap items-baseline justify-between gap-2">
                <div className="flex items-baseline gap-2">
                  <Heading as="h3" size="sm">{t('Top flows')}</Heading>
                  <span className="text-xs text-muted-foreground/80">{t('Derived')}</span>
                </div>
                <span className="text-xs text-muted-foreground">
                  {hidden > 0
                    ? t('Showing {shown} of {total} flows in the response.', { shown: visible.length, total: matched.length })
                    : t('Showing all {total} flows in the response.', { total: matched.length })}
                </span>
              </div>

              <ul className="divide-y divide-border">
                {visible.map((flow, index) => {
                  const peer = flowPeer(flow, client)
                  const peerText = peerLabel(peer) ?? flowEndpoints(flow) ?? '—'
                  const directions = flowDirections(flow)
                  const rateText = [
                    directions.down == null ? undefined : `↓ ${formatRate(directions.down / 8)}`,
                    directions.up == null ? undefined : `↑ ${formatRate(directions.up / 8)}`,
                  ].filter(Boolean).join(' · ') || '—'
                  const share = flowShare(flow, client)
                  return <li key={`${flow.proto}-${flow.src}-${flow.sport}-${flow.dst}-${flow.dport}-${index}`} className="py-2.5">
                    <div className="flex items-baseline justify-between gap-3">
                      <span className="min-w-0 break-all font-mono text-xs leading-5">{peerText}</span>
                      <span className="shrink-0 font-mono text-xs font-medium tabular-nums">{rateText}</span>
                    </div>
                    <div className="mt-1 flex flex-wrap items-baseline gap-x-3 gap-y-0.5 text-xs text-muted-foreground">
                      <span className="font-mono uppercase">{flow.proto ?? '—'}</span>
                      <span>{flow.state ?? '—'}</span>
                      <span className="ml-auto tabular-nums">{t('Share')} {share == null ? '—' : formatPercent(share)}</span>
                    </div>
                  </li>
                })}
              </ul>

              {matched.length > visible.length && <Button variant="quiet" size="sm" onClick={() => setExpanded(true)}>
                {t('{count} more', { count: matched.length - visible.length })}
              </Button>}
              {expanded && matched.length > FLOW_ROW_LIMIT && <Button variant="quiet" size="sm" onClick={() => setExpanded(false)}>
                {t('Show fewer')}
              </Button>}

              <div className="space-y-1 border-t border-border pt-2 text-xs leading-4 text-muted-foreground">
                {hidden > 0 && <p>{t('{count} more of this client’s flows in the response are not shown.', { count: hidden })}</p>}
                {measured && reading && apiCapReached(reading.flows.length, FLOW_API_LIMIT) && <p>{t('The API returned its {count}-flow limit; flows below that cut are not shown.', { count: FLOW_API_LIMIT })}</p>}
                <p>{shareNote}</p>
                {total == null && <p>{t('Shares are unavailable because the client total was not reported.')}</p>}
                {total === 0 && <p>{t('Shares are unavailable because the client total is zero.')}</p>}
              </div>
            </>}
      </div>
    </SheetContent>
  </Sheet>
}
