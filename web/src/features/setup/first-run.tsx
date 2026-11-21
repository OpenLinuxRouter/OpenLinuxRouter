import { t, useLocale } from '@/lib/i18n'
import { Check, ChevronRight } from 'lucide-react'
import { Link } from 'react-router'

import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { useDhcpConfig } from '@/features/dhcp/queries'
import { ApplyOutcome } from '@/features/dns/editor'
import { useDnsConfig } from '@/features/dns/queries'
import { useDnsApply } from '@/features/dns/use-apply'
import { useInterfaces } from '@/features/link/queries'
import { cn } from '@/lib/utils'

/**
 * What to do next, on a box that has not been told to do anything yet.
 *
 * docs/system.md §1.3 settles what the *claim* screen is — one question, not a
 * wizard — and then hands the operator a dashboard reporting on a router that
 * is, so far, not routing anything: no interface adopted, no addresses handed
 * out, no names answered. Everything works and nothing is happening, which
 * reads as a broken install rather than a blank one.
 *
 * So this is not a wizard either. It is three rows on the page they already
 * landed on, each naming a thing the box cannot do yet and going straight to
 * where it is turned on. It is derived entirely from state and stores no
 * "dismissed" flag: there is nothing to dismiss, because it stops rendering the
 * moment the box is doing something.
 *
 * That last part is deliberate and is what keeps it from nagging. A box
 * deliberately serving DNS and no DHCP is a supported arrangement, not an
 * unfinished setup, so the panel leaves as soon as *either* service is on
 * rather than waiting for all three ticks.
 */
export function FirstRun() {
  useLocale()
  const interfaces = useInterfaces()
  const dhcp = useDhcpConfig()
  const dns = useDnsConfig()
  const applier = useDnsApply()

  // Wait for all three rather than flashing a setup panel at somebody whose
  // configured box is merely slow to answer.
  if (!interfaces.data || !dhcp.data || !dns.data) return null

  const adopted = interfaces.data.interfaces.filter((i) => i.adopted)
  const networks = interfaces.data.networks
  const serving = dhcp.data.enabled || dns.data.enabled
  if (adopted.length > 0 && serving) return null

  return (
    <Card>
      {/* Turning DNS on from here goes through the same plan-then-apply path as
          the switch on the DNS page, so the same confirmation has to be
          reachable from here. Starting a resolver is not a disruptive plan
          today, but "today" is not a thing to build a button on. */}
      <ApplyOutcome applier={applier} />

      <CardHeader>
        <CardTitle>{t('Finish setting up')}</CardTitle>
        <CardDescription>{t('Choose the services this router should provide.')}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-1">
        {/* One step for both halves, because they are one page and neither is
            any use alone: an adopted interface with no network carries no
            address, and a network cannot be created on an interface nobody
            handed over. Splitting them into two rows described the same trip
            twice. */}
        <Step
          n={1}
          done={adopted.length > 0 && networks.length > 0}
          title={t('Configure an interface')}
          detail={
            adopted.length === 0
              ? 'Choose an interface, then configure its network.'
              : networks.length === 0
                ? `${adopted.map((i) => i.name).join(', ')} — yours to configure. Now say what subnet it serves.`
                : networks.map((n) => `${n.name} on ${n.members.join(', ')}`).join(' · ')
          }
          action={{
            to: adopted.length ? '/gateway/dhcp' : '/gateway/interfaces',
            label: adopted.length && networks.length ? t('Change') : 'Set it up',
          }}
        />
        <Step
          n={2}
          done={dhcp.data.enabled && (dhcp.data.pools?.length ?? 0) > 0}
          // Blocked on the network, not just on adoption: a range is stored
          // against a network and the form has nothing to offer until one
          // exists. Pointing somebody at it earlier sends them to an empty
          // select box.
          blocked={networks.length === 0}
          title={t('Set up an address range')}
          // Not a one-click, unlike DNS below, and the reason is the trap
          // docs/install.md §4 spends two paragraphs on: the gateway and DNS a
          // range advertises default to *this* box, which is right when it is
          // your router and catastrophic when your old one still is. The form
          // asks; a button here could only guess.
          detail="Check the advertised gateway and DNS if another router is still serving this network."
          action={{ to: '/gateway/dhcp/ranges', get label() { return t('Set up a range') } }}
        />
        <Step
          n={3}
          done={dns.data.enabled}
          blocked={adopted.length === 0}
          title={t('Enable DNS')}
          detail="Resolve and log DNS requests through OLR."
          action={{
            get label() { return t('Turn on') },
            // Safe as one click, where the range above is not: nothing uses
            // this resolver until something is pointed at it, and turning it on
            // now picks its own listen address (internal/dns derive.go).
            onClick: () => void applier.submit({ ...dns.data, enabled: true }),
            busy: applier.busy,
          }}
        />
      </CardContent>
    </Card>
  )
}

function Step({
  n,
  done,
  blocked,
  title,
  detail,
  action,
}: {
  n: number
  done: boolean
  /** True while an earlier step has to happen first. */
  blocked?: boolean
  title: string
  detail: string
  action: { label: string; to?: string; onClick?: () => void; busy?: boolean }
}) {
  useLocale()
  const control = !done && (action.to ? (
    <Button nativeButton={false} size="sm" variant={blocked ? 'quiet' : 'secondary'} disabled={blocked} render={
      <Link to={action.to}>
        {action.label}
        <ChevronRight className="size-3.5" aria-hidden />
      </Link>
    } />
  ) : (
    <Button
      size="sm"
      variant={blocked ? 'quiet' : 'secondary'}
      disabled={blocked || action.busy}
      onClick={action.onClick}
    >
      {action.label}
    </Button>
  ))

  return (
    <div className="flex items-start gap-3 py-2">
      <span
        className={cn(
          'mt-0.5 flex size-6 shrink-0 items-center justify-center rounded-full text-xs font-medium',
          done
            ? 'bg-success/15 text-success-foreground'
            : blocked
              ? 'bg-muted text-muted-foreground/60'
              : 'bg-primary/10 text-primary',
        )}
        aria-hidden
      >
        {done ? <Check className="size-3.5" /> : n}
      </span>

      {/* The step's action sits beside its text on a desktop and under it on a
          phone. Sharing one line on a 390px screen squeezed the title until it
          wrapped and left three different action edges down the panel. */}
      <div className="flex min-w-0 flex-1 flex-col gap-2 sm:flex-row sm:items-start sm:gap-3">
        <div className="min-w-0 flex-1">
          <div className={cn('text-sm font-medium', blocked && !done && 'text-muted-foreground')}>
            {title}
          </div>
          <p className="text-xs text-muted-foreground">{detail}</p>
        </div>
        {control && <div className="self-start sm:ml-auto sm:shrink-0">{control}</div>}
      </div>
    </div>
  )
}
