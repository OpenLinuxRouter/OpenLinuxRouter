import { t, useLocale } from '@/lib/i18n'
import { useLayoutEffect, type CSSProperties } from 'react'
import { Link, NavLink, Outlet, matchPath, useLocation } from 'react-router'

import { FooterLine, RailFooter } from '@/components/layout/console-footer'
import { Logo } from '@/components/layout/logo'
import { SECTIONS, sectionOf } from '@/components/layout/sections'
import { TokenButton } from '@/components/layout/token-button'
import {
  Sidebar,
  SidebarContent,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarProvider,
  SidebarRail,
} from '@/components/ui/sidebar'
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
        </SidebarHeader>

        <SidebarContent>
          <nav aria-label={t('Sections')}>
            <RailGroup label={t('Network')} sections={network} active={active} />
            <RailGroup label={t('Configuration')} sections={configuration} active={active} />
          </nav>
        </SidebarContent>

        <RailFooter />
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

        <FooterLine />

        <nav aria-label={t('Sections')} className="fixed inset-x-0 bottom-0 z-20 border-t bg-card/95 pb-[env(safe-area-inset-bottom)] backdrop-blur-xl lg:hidden">
          <ul className="grid grid-cols-6">
            {/* Objects first, then configuration, so the bar and the rail tell the
                same story even though the table interleaves them by module. */}
            {[...network, ...configuration].map(({ to, label, icon: Icon, end }) => (
              // Six names across a 375px phone: the one place below 12px, so the
              // longest label still fits its cell (docs/ui-style-guide.md).
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

/** shadcn's provider writes the choice to a cookie; read it back on load. */
function railOpenByDefault() {
  return !document.cookie.split('; ').includes('sidebar_state=false')
}
