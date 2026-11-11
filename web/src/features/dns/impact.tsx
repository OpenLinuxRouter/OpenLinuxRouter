import { t, useLocale } from '@/lib/i18n'
import {
  type ImpactLabels,
  ImpactBadge as Badge,
  PlanDiff as Diff,
  impactHint as hint,
} from '@/features/plan/plan-preview'
import type { Impact, PlanCore } from '@/lib/api-types'

export { PlanReasons } from '@/features/plan/plan-preview'

/**
 * The same four classes as dhcp's, weighted differently — and the difference is
 * the point.
 *
 * `restart` is *cheaper* here: DNS is stateless and clients retry unprompted, so
 * bouncing a resolver costs milliseconds and nobody notices. That is exactly
 * why internal/dns/plan.go concludes owning :53 is survivable.
 *
 * `disruptive` is far *worse* here. A DHCP server going away costs nobody
 * anything until their lease expires; a resolver going away costs everybody
 * everything at once, and it does not present as "DNS is down" — it presents as
 * the internet being broken. The label says that in the words the person will
 * actually use.
 */
export const IMPACT: ImpactLabels = {
  none: {
    get label() { return t('No change') },
    get hint() { return t('Nothing running changes.') },
    variant: 'secondary',
  },
  reload: {
    get label() { return t('Seamless') },
    hint: 'Picked up without interrupting a single lookup.',
    variant: 'success',
  },
  restart: {
    get label() { return t('Unnoticed') },
    hint: 'A lookup in flight is retried automatically. In practice nobody sees this.',
    variant: 'warning',
  },
  disruptive: {
    get label() { return t('Internet stops working') },
    hint: 'Clients lose name resolution. Nothing will load until this is undone — it will not look like a DNS problem, it will look like the internet is down.',
    variant: 'destructive',
  },
}

export function ImpactBadge({ impact, className }: { impact: Impact; className?: string }) {
  useLocale()
  return <Badge impact={impact} labels={IMPACT} className={className} />
}

export function impactHint(impact: Impact): string {
  return hint(IMPACT, impact)
}

export function PlanDiff({ plan }: { plan: PlanCore }) {
  useLocale()
  return <Diff plan={plan} labels={IMPACT} />
}
