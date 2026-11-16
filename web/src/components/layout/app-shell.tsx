import { t, useLocale } from '@/lib/i18n'
import { PanelLeft } from 'lucide-react'
import { useLayoutEffect, type CSSProperties } from 'react'
import { Link, NavLink, Outlet, matchPath, useLocation } from 'react-router'

import { GithubMark } from '@/components/layout/github-mark'
import { Logo } from '@/components/layout/logo'
import { SECTIONS, sectionOf } from '@/components/layout/sections'
import { ThemeToggle } from '@/components/layout/theme-toggle'
import { LanguageToggle } from '@/components/layout/language-toggle'
import { TokenButton } from '@/components/layout/token-button'
import { Button } from '@/components/ui/button'
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarProvider,
  SidebarRail,
  useSidebar,
} from '@/components/ui/sidebar'
import { StatusInline } from '@/components/ui/status'
import { useHostMetrics } from '@/features/system/queries'
import { ApiError } from '@/lib/api'
import { state } from '@/lib/state'
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
  const active = ({ to, end }: { to: string, end: boolean }) => owns(to) || matchPath({ path: to, end }, pathname) !== null

  useLayoutEffect(() => {
    if (!hash) window.scrollTo({ top: 0, left: 0, behavior: 'instant' })
  }, [pathname, hash])

  const top = SECTIONS.filter((s) => !s.nested)
  const network = top.filter((s) => s.group === 'network')
  const configuration = top.filter((s) => s.group !== 'network')

  return (
    <SidebarProvider defaultOpen={railOpenByDefault()} className="bg-background text-foreground" style={{ '--sidebar-width': '15.5rem' } as CSSProperties}>
      <a href="#main-content" className="fixed top-2 left-4 z-50 rounded-lg bg-primary-fill px-4 py-3 text-primary-foreground -translate-y-24 focus:translate-y-0">{t('Skip to content')}</a>

      {/* shadcn's Sidebar, desktop only: below lg the bottom bar is the
          navigation and the rail's sheet is never opened. Collapsing leaves the
          icons, each with its name in a tooltip. */}
      <Sidebar collapsible="icon">
        <SidebarHeader className="px-3 pt-5 pb-2 group-data-[collapsible=icon]:px-2">
          <SidebarMenu>
            <SidebarMenuItem>
              <SidebarMenuButton size="lg" className="h-9 gap-2.5 hover:bg-transparent active:bg-transparent" tooltip="Open Linux Router"
                render={<Link to="/" aria-label={t('Open Linux Router overview')} />}>
                <Logo className="size-5! shrink-0" aria-hidden />
                <span className="text-sm font-semibold tracking-tight group-data-[collapsible=icon]:hidden">Open Linux Router</span>
              </SidebarMenuButton>
            </SidebarMenuItem>
          </SidebarMenu>
          {/* The mark's two colours, drawn once. They are identity, not a token
              with a role: never a state, never a series, never a fill (console
              direction §5). */}
          <span aria-hidden className="ml-2 block h-0.5 w-[34px] rounded-full bg-[linear-gradient(90deg,var(--brand-a),var(--brand-b))] group-data-[collapsible=icon]:hidden" />
          <RouterLink />
        </SidebarHeader>

        <SidebarContent>
          <nav aria-label={t('Sections')}>
            <RailGroup label={t('Network')} sections={network} active={active} />
            <RailGroup label={t('Configuration')} sections={configuration} active={active} />
          </nav>
        </SidebarContent>

        <SidebarFooter className="flex-row items-center justify-end gap-1 border-t px-3 py-2.5 group-data-[collapsible=icon]:flex-col group-data-[collapsible=icon]:px-2">
          <TokenButton />
          <RailToggle />
        </SidebarFooter>
        <SidebarRail aria-label={t('Collapse or expand the sidebar')} title={t('Collapse or expand the sidebar')} />
      </Sidebar>

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
          <ul className="grid grid-cols-7">
            {/* Objects first, then configuration, so the bar and the rail tell the
                same story even though the table interleaves them by module. */}
            {[...network, ...configuration].map(({ to, label, icon: Icon, end }) => (
              // Seven names across 375px: the one place below 12px, because at 12px
              // the labels run into each other (docs/ui-style-guide.md).
              <li key={to} className="min-w-0">
                <NavLink to={to} end={end} className={({ isActive }) => cn(
                  'flex min-h-16 min-w-0 flex-col items-center justify-center gap-1 text-[11px] font-medium transition-colors focus-visible:outline-2 focus-visible:-outline-offset-4 focus-visible:outline-ring',
                  (isActive || owns(to)) ? 'text-primary' : 'text-muted-foreground hover:text-foreground',
                )}><Icon className="size-5" aria-hidden />{label}</NavLink>
              </li>
            ))}
          </ul>
        </nav>
      </div>
    </SidebarProvider>
  )
}

/** One named group of the rail: a heading and the destinations under it. */
function RailGroup({ label, sections, active }: {
  label: string
  sections: typeof SECTIONS
  active: (section: (typeof SECTIONS)[number]) => boolean
}) {
  if (sections.length === 0) return null
  return (
    <SidebarGroup className="px-3 py-1 group-data-[collapsible=icon]:px-2">
      {/* Muted is a token, not an opacity: shadcn's foreground/70 would put a
          second grey in the rail beside every other label's muted-foreground. */}
      <SidebarGroupLabel render={<h2 />} className="text-muted-foreground">{label}</SidebarGroupLabel>
      <SidebarGroupContent>
        <SidebarMenu className="gap-0.5">
          {sections.map((section) => {
            const { to, label: itemLabel, icon: Icon, end } = section
            const selected = active(section)
            return (
              <SidebarMenuItem key={to}>
                {/* Selection is the primary/10 pill of the style guide rather than
                    shadcn's neutral accent: a tinted shape, so it survives without
                    reading the hue, in the one interaction colour. */}
                <SidebarMenuButton isActive={selected} tooltip={itemLabel}
                  className="gap-2.5 data-active:bg-primary/10 data-active:text-primary dark:data-active:bg-primary/15 data-active:hover:bg-primary/10 dark:data-active:hover:bg-primary/15 data-active:hover:text-primary"
                  render={<NavLink to={to} end={end} />}>
                  <Icon className="text-muted-foreground group-data-active/menu-button:text-primary" aria-hidden />
                  <span>{itemLabel}</span>
                </SidebarMenuButton>
              </SidebarMenuItem>
            )
          })}
        </SidebarMenu>
      </SidebarGroupContent>
    </SidebarGroup>
  )
}

/**
 * Which router this console is talking to, and whether it is still answering
 * (console direction §6.1). The host is the address in the bar — the daemon
 * reports no name of its own — and the state comes from the five-second
 * metrics read the Status card already makes, so it costs no request.
 *
 * Any HTTP answer, even an error or a 401, means the daemon is there; only a
 * request that never got one is Not answering.
 */
function RouterLink() {
  const metrics = useHostMetrics()
  const health = metrics.isPending
    ? state('Checking…')
    : metrics.isError && !(metrics.error instanceof ApiError)
      ? state('Not answering')
      : state('Connected')
  const host = window.location.host
  return (
    <div className="mt-1 flex min-w-0 flex-col gap-0.5 px-2 group-data-[collapsible=icon]:items-center group-data-[collapsible=icon]:px-0"
      title={`${host} · ${health.label}`}>
      <span className="truncate font-mono text-xs text-muted-foreground group-data-[collapsible=icon]:hidden">{host}</span>
      <StatusInline tone={health.tone} emphasis="dense" className="text-xs group-data-[collapsible=icon]:[&>span:last-child]:sr-only">
        {health.label}
      </StatusInline>
    </div>
  )
}

/** Collapses the rail to its icons; ⌘B / Ctrl+B does the same. */
function RailToggle() {
  const { open, toggleSidebar } = useSidebar()
  const label = open ? t('Collapse sidebar') : t('Expand sidebar')
  return (
    <Button variant="quiet" size="icon-sm" onClick={toggleSidebar} aria-label={label} title={label} className="text-muted-foreground">
      <PanelLeft aria-hidden />
    </Button>
  )
}

/** shadcn's provider writes the choice to a cookie; read it back on load. */
function railOpenByDefault() {
  return !document.cookie.split('; ').includes('sidebar_state=false')
}
