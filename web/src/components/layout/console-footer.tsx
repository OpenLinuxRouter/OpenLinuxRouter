import { PanelLeft } from 'lucide-react'
import { Link } from 'react-router'

import { GithubMark } from '@/components/layout/github-mark'
import { LanguageToggle } from '@/components/layout/language-toggle'
import { ThemeToggle } from '@/components/layout/theme-toggle'
import { TokenButton } from '@/components/layout/token-button'
import { Button } from '@/components/ui/button'
import {
  SidebarFooter,
  useSidebar,
} from '@/components/ui/sidebar'
import { t } from '@/lib/i18n'
import { cn } from '@/lib/utils'

/**
 * The facts every surface repeats: what build this is, and the three controls
 * that belong to the console rather than to a page.
 *
 * On a phone the footer is one row above the tab bar. From lg the rail has no
 * room for a row that wide, so the same items stand in its foot: the controls on
 * one line, the legal line under them, and both go when the rail collapses to
 * 48px of icons — a rail that narrow cannot set a version string, and the names
 * come back with the labels (⌘B). The copyright drops the product name in the
 * rail for the same reason it drops it on a phone: the wordmark is already
 * above it.
 */

/** GitHub's mark, alone: the anchor carries no word, so the labels name it. */
function GitHubLink({ className }: { className?: string }) {
  return (
    <a href="https://github.com/open-linux-router/open-linux-router" target="_blank" rel="noreferrer" aria-label="GitHub" title="GitHub"
      className={cn('inline-flex shrink-0 items-center justify-center rounded-md text-muted-foreground transition-colors duration-150 hover:text-foreground focus-visible:outline-2 focus-visible:outline-ring', className)}>
      {/* One icon size for the row. The mark's artwork carries its own optical
          inset, so it takes the same 14px as the theme control rather than a
          smaller box that merely looks the same. */}
      <GithubMark className="size-3.5" />
    </a>
  )
}

/** The year, and the product's name where the surface has room to repeat it. */
function Copyright({ product = false }: { product?: boolean }) {
  return (
    <span>
      © {new Date().getFullYear()}
      {/* Below 640px the header already carries the product name, and the row
          cannot fit it; the rail never shows it, because the wordmark is above. */}
      {product && <span className="hidden sm:inline"> Open Linux Router</span>}
    </span>
  )
}

function CreditsLink({ className }: { className?: string }) {
  return (
    <Link to="/credits" className={cn('rounded-sm transition-colors duration-150 hover:text-foreground focus-visible:outline-2 focus-visible:outline-ring', className)}>
      {t('Credits')}
    </Link>
  )
}

/** The version stays on every surface: it is the one thing only the footer
 *  reports, and the one thing a bug report always asks for. */
function Version() {
  return <span>{__APP_VERSION__}</span>
}

/**
 * The phone's footer. From lg the rail's foot carries these items, so this one
 * is the only copy below that width; the bottom padding clears the fixed tab bar
 * it stands behind.
 */
export function FooterLine() {
  return (
    <footer className="lg:hidden">
      {/* One line at every width, so the row centres instead of wrapping and the
          gap tightens only under 360px. Each control is 44px tall with 8px of side
          padding, so its hit area reaches into the gap but stops at the midpoint
          instead of overlapping its neighbour; cancel that padding here, and the
          visible gap stays 16px while every control stays 44px tall to the
          pointer. border-0 keeps the buttons' 1px border from lifting their
          content off the line the plain items share. */}
      <div className="mx-auto flex max-w-[80rem] items-center justify-center gap-x-3 px-4 pt-8 pb-[calc(6rem+env(safe-area-inset-bottom))] text-center text-xs text-muted-foreground min-[360px]:gap-x-4">
        <Copyright product />
        <CreditsLink className="inline-flex min-h-11 items-center" />
        <Version />
        <GitHubLink className="-mx-2 h-11 px-2" />
        <ThemeToggle className="-mx-2 border-0" />
        <LanguageToggle className="-mx-2 border-0" />
      </div>
    </footer>
  )
}

/**
 * The rail's foot. The controls sit on one 32px line — the rail's own control
 * size — and the legal line under them, so the last thing the eye meets is what
 * this build is rather than a toolbar.
 */
export function RailFooter() {
  return (
    <SidebarFooter className="gap-1 border-t px-3 py-2.5 group-data-[collapsible=icon]:items-center group-data-[collapsible=icon]:gap-1 group-data-[collapsible=icon]:px-2">
      <div className="flex w-full items-center gap-0.5 group-data-[collapsible=icon]:flex-col group-data-[collapsible=icon]:gap-1">
        <GitHubLink className="size-8" />
        <ThemeToggle className="size-8 border-0" />
        <LanguageToggle className="h-8 px-2 group-data-[collapsible=icon]:w-8 group-data-[collapsible=icon]:px-0" />
        <TokenButton className="size-8" />
        <RailToggle />
      </div>
      <div className="flex w-full flex-wrap items-center gap-x-2.5 gap-y-1 px-2 text-xs text-muted-foreground group-data-[collapsible=icon]:hidden">
        <Copyright />
        <CreditsLink />
        <Version />
      </div>
    </SidebarFooter>
  )
}

/** Collapses the rail to its icons; ⌘B / Ctrl+B does the same. */
function RailToggle() {
  const { open, toggleSidebar } = useSidebar()
  const label = open ? t('Collapse sidebar') : t('Expand sidebar')
  return (
    <Button variant="quiet" size="icon-sm" onClick={toggleSidebar} aria-label={label} title={label}
      className="ml-auto text-muted-foreground group-data-[collapsible=icon]:mx-auto">
      <PanelLeft aria-hidden />
    </Button>
  )
}
