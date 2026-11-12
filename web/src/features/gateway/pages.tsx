import { t, useLocale } from '@/lib/i18n'
import { NavLink } from 'react-router'

import { cn } from '@/lib/utils'

import { GATEWAY_PAGES } from './catalog'

/**
 * The pages every choice on this screen is written against.
 *
 * They wear a tab strip's shape but they are links: each one loads its own page
 * with its own data, so `aria-current` names the page you are on and nothing
 * ever switches a panel in place. The strip keeps the interfaces, the route
 * table and the unmanaged rules one click from the landing page, without a
 * group whose tiles would all leave the directory.
 */
export function GatewayPages() {
  useLocale()
  return (
    <nav aria-label={t('Gateway pages')} className="flex gap-1 overflow-x-auto border-b border-border">
      {GATEWAY_PAGES.map(page => (
        <NavLink
          key={page.id}
          to={page.to}
          className={({ isActive }) => cn(
            '-mb-px shrink-0 border-b-2 px-3 py-2 text-sm whitespace-nowrap transition-colors duration-150 focus-visible:outline-2 focus-visible:outline-ring',
            isActive
              ? 'border-primary font-medium text-primary'
              : 'border-transparent text-muted-foreground hover:text-foreground',
          )}
        >
          {page.name}
        </NavLink>
      ))}
    </nav>
  )
}
