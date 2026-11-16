import { t, useLocale } from '@/lib/i18n'
import { useLayoutEffect } from 'react'
import { Link, NavLink, Outlet, useLocation } from 'react-router'

import { GithubMark } from '@/components/layout/github-mark'
import { Logo } from '@/components/layout/logo'
import { SECTIONS, sectionOf } from '@/components/layout/sections'
import { ThemeToggle } from '@/components/layout/theme-toggle'
import { LanguageToggle } from '@/components/layout/language-toggle'
import { TokenButton } from '@/components/layout/token-button'
import { cn } from '@/lib/utils'

/**
 * The shell puts the objects an operator looks at first and the modules that
 * configure them second (docs/ui-console-direction.md §2). Desktop gets a rail
 * with those two groups; phones keep the top bar and the bottom tab bar, which
 * has nowhere to put a second group and does not need one.
 */
export function AppShell() {
  useLocale()
  const { pathname, hash } = useLocation()
  const section = sectionOf(pathname)
  const landing = section && pathname === section.to
  // Detail pages keep their owning section selected in both navigation bars.
  const owns = (to: string) => section?.to === to || section?.navTo === to

  useLayoutEffect(() => {
    if (!hash) window.scrollTo({ top: 0, left: 0, behavior: 'instant' })
  }, [pathname, hash])

  const top = SECTIONS.filter((s) => !s.nested)
  const network = top.filter((s) => s.group === 'network')
  const configuration = top.filter((s) => s.group !== 'network')

  const itemClass = (active: boolean) =>
    cn(
      'flex h-8 items-center gap-2.5 rounded-lg px-2 text-[13px] transition-colors duration-150 focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-ring',
      active
        ? 'bg-primary/10 font-medium text-primary dark:bg-primary/15'
        : 'text-foreground hover:bg-accent/60',
    )

  return (
    <div className="flex min-h-svh bg-background text-foreground">
      <a href="#main-content" className="fixed top-2 left-4 z-50 rounded-lg bg-primary-fill px-4 py-3 text-primary-foreground -translate-y-24 focus:translate-y-0">{t('Skip to content')}</a>

      <aside className="sticky top-0 hidden h-svh w-[15.5rem] shrink-0 flex-col border-r bg-card lg:flex">
        <div className="px-3 pt-5 pb-4">
          <Link to="/" className="inline-flex min-h-9 items-center gap-2.5 rounded-md focus-visible:outline-2 focus-visible:outline-ring" aria-label={t('Open Linux Router overview')}>
            <Logo className="size-5 shrink-0" aria-hidden />
            <span className="text-[13.5px] font-semibold tracking-tight">Open Linux Router</span>
          </Link>
          {/* The mark's two colours, drawn once. They are identity, not a token
              with a role: never a state, never a series, never a fill (console
              direction §5). */}
          <span aria-hidden className="mt-2.5 block h-0.5 w-[34px] rounded-full bg-[linear-gradient(90deg,var(--brand-a),var(--brand-b))]" />
        </div>

        <nav aria-label={t('Sections')} className="min-h-0 flex-1 overflow-y-auto px-3 pb-4">
          <RailGroup label={t('Network')} sections={network} owns={owns} itemClass={itemClass} />
          <RailGroup label={t('Configuration')} sections={configuration} owns={owns} itemClass={itemClass} />
        </nav>

        <div className="flex items-center justify-end gap-1 border-t px-3 py-2.5">
          <TokenButton />
        </div>
      </aside>

      <div className="flex min-w-0 flex-1 flex-col">
        <header className="sticky top-0 z-20 border-b bg-card/95 backdrop-blur-xl lg:hidden">
          <div className="mx-auto flex h-16 max-w-[80rem] items-center gap-2.5 px-4 sm:px-6">
            <Link to="/" className="inline-flex min-h-11 shrink-0 items-center gap-2.5 rounded-md focus-visible:outline-2 focus-visible:outline-ring" aria-label={t('Open Linux Router overview')}>
              <Logo className="size-5 shrink-0" aria-hidden />
              <span className="font-semibold tracking-tight">Open Linux Router</span>
            </Link>
            <div className="ml-auto flex items-center gap-1"><TokenButton /></div>
          </div>
        </header>

        <main id="main-content" tabIndex={-1} className="mx-auto w-full max-w-[80rem] flex-1 px-4 py-6 outline-none sm:px-6 sm:py-8 xl:px-8">
          <div className="min-w-0">
            {landing && !section.nested && <h1 className="sr-only">{pathname === '/' ? t('Network overview') : section.label}</h1>}
            <Outlet />
          </div>
        </main>

        <footer>
          {/* One line at every width, so the row centres instead of wrapping and the
              gap tightens only under 360px. The product name leaves the copyright on
              phones, where the header already carries it and the six items cannot fit;
              the version stays, because it is the one thing the footer alone reports. */}
          <div className="mx-auto flex max-w-[80rem] items-center justify-center gap-x-3 px-4 pt-8 pb-[calc(6rem+env(safe-area-inset-bottom))] text-center text-xs text-muted-foreground min-[360px]:gap-x-4 lg:items-end lg:pb-3">
            <span>© {new Date().getFullYear()}<span className="hidden sm:inline"> Open Linux Router</span></span>
            <Link to="/credits" className="inline-flex min-h-11 items-center transition-colors duration-150 hover:text-foreground lg:items-end">{t('Credits')}</Link>
            <span>{__APP_VERSION__}</span>
            {/* Each control is 44px tall with 8px of side padding, so its hit area
                reaches into the gap but stops at the midpoint instead of overlapping
                its neighbour. Cancel that padding here, and the items keep one visible
                gap while every control stays 44px tall to the pointer. border-0 keeps
                the buttons' 1px border from lifting their content off the line the
                plain items share. */}
            <a href="https://github.com/open-linux-router/open-linux-router" target="_blank" rel="noreferrer" aria-label="GitHub" title="GitHub"
              className="inline-flex h-11 shrink-0 items-center justify-center px-2 -mx-2 transition-colors duration-150 hover:text-foreground focus-visible:outline-2 focus-visible:outline-ring lg:items-end">
              {/* One icon size for the row. The mark's artwork carries its own
                  optical inset, so it takes the same 14px as the theme control
                  rather than a smaller box that merely looks the same. */}
              <GithubMark className="size-3.5" />
            </a>
            <ThemeToggle className="-mx-2 border-0" />
            {/* The word reports the language in use, so it is always set in the script
                of the words and marks beside it and shares their line, instead of being
                the one word in a language the row is not set in. */}
            <LanguageToggle className="-mx-2 border-0" />
          </div>
        </footer>

        <nav aria-label={t('Sections')} className="fixed inset-x-0 bottom-0 z-20 border-t bg-card/95 pb-[env(safe-area-inset-bottom)] backdrop-blur-xl lg:hidden">
          <ul className="grid grid-cols-6">
            {top.map(({ to, label, icon: Icon, end }) => (
              <li key={to} className="min-w-0">
                <NavLink to={to} end={end} className={({ isActive }) => cn(
                  'flex min-h-16 min-w-0 flex-col items-center justify-center gap-1 text-[0.625rem] font-medium transition-colors focus-visible:outline-2 focus-visible:-outline-offset-4 focus-visible:outline-ring',
                  (isActive || owns(to)) ? 'text-primary' : 'text-muted-foreground hover:text-foreground',
                )}><Icon className="size-5" aria-hidden />{label}</NavLink>
              </li>
            ))}
          </ul>
        </nav>
      </div>
    </div>
  )
}

/** One named group of the rail: a heading and the destinations under it. */
function RailGroup({ label, sections, owns, itemClass }: {
  label: string
  sections: typeof SECTIONS
  owns: (to: string) => boolean
  itemClass: (active: boolean) => string
}) {
  if (sections.length === 0) return null
  return (
    <div className="pt-4 first:pt-1">
      <h2 className="px-2 pb-1.5 text-[11px] font-medium text-muted-foreground">{label}</h2>
      <ul className="flex flex-col gap-0.5">
        {sections.map(({ to, label: itemLabel, icon: Icon, end }) => (
          <li key={to}>
            <NavLink to={to} end={end} className={({ isActive }) => itemClass(isActive || owns(to))}>
              {({ isActive }) => (
                <>
                  <Icon className={cn('size-4 shrink-0', isActive || owns(to) ? 'text-primary' : 'text-muted-foreground')} aria-hidden />
                  <span className="truncate">{itemLabel}</span>
                </>
              )}
            </NavLink>
          </li>
        ))}
      </ul>
    </div>
  )
}
