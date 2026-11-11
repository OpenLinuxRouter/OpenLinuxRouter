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
 * What each impact class costs the person holding the phone, in dhcp's terms.
 *
 * Every label is about addresses, because that is all this module hands out.
 * dns has its own set for the same four classes, and the two must not be merged:
 * losing an address and losing name resolution are not the same event, and one
 * of them is survivable.
 */
export const IMPACT: ImpactLabels = {
  none: {
    get label() { return t('No change') },
    get hint() { return t('Nothing running changes.') },
    variant: 'secondary',
  },
  reload: {
    get label() { return t('Seamless') },
    get hint() { return t('Clients stay connected and notice nothing.') },
    variant: 'success',
  },
  restart: {
    get label() { return t('Brief pause') },
    get hint() { return t('Address handling stops for a moment. Clients keep the addresses they already have.') },
    variant: 'warning',
  },
  disruptive: {
    get label() { return t('Clients disconnect') },
    hint: 'Clients using an address from this router will lose it.',
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
