import { MoreHorizontal, PanelLeft } from 'lucide-react'
import { useTheme } from 'next-themes'
import { useState } from 'react'
import { Link } from 'react-router'

import { GithubMark } from '@/components/layout/github-mark'
import { LanguageToggle } from '@/components/layout/language-toggle'
import { ThemeToggle } from '@/components/layout/theme-toggle'
import { TokenDialog, TokenMenuItem } from '@/components/layout/token-button'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import {
  SidebarFooter,
  useSidebar,
} from '@/components/ui/sidebar'
import { setLocale, t, useLocale, type Locale } from '@/lib/i18n'
import { cn } from '@/lib/utils'

/**
 * The console's own items: what build this is, and the settings that belong to
 * the console rather than to a page.
 *
 * A phone has no rail, so below `lg` these are one centred row above the tab
 * bar. From `lg` the page column ends with content and the rail's foot carries
 * them instead, because a rail has one strip at the bottom and it is better
 * spent on a menu than on six rarely used controls standing in a row. That foot
 * is the version — a button that opens the console menu — and the collapse
 * toggle; the menu holds the theme, the language, the links, the credential
 * when one is set, and the copyright. The version stays on the trigger rather
 * than in the menu because it is what a bug report asks for.
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

/**
 * The phone's footer. From lg the rail's foot carries the console's own items,
 * so this one is the only copy below that width; the bottom padding clears the
 * fixed tab bar it stands behind.
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
        <span>© {new Date().getFullYear()}<span className="hidden sm:inline"> Open Linux Router</span></span>
        <Link to="/credits" className="inline-flex min-h-11 items-center transition-colors duration-150 hover:text-foreground">{t('Credits')}</Link>
        <span>{__APP_VERSION__}</span>
        <GitHubLink className="-mx-2 h-11 px-2" />
        <ThemeToggle className="-mx-2 border-0" />
        <LanguageToggle className="-mx-2 border-0" />
      </div>
    </footer>
  )
}

/**
 * The rail's foot. One quiet line that names the build and opens the console's
 * menu, and the collapse toggle beside it.
 */
export function RailFooter() {
  // The credential dialog is a sibling of the menu, not a row inside it: the
  // menu unmounts on select, and a dialog mounted in its popup would go with it.
  const [tokenOpen, setTokenOpen] = useState(false)
  return (
    <SidebarFooter className="gap-0 border-t px-2 py-2 group-data-[collapsible=icon]:px-2">
      <div className="flex w-full items-center gap-1 group-data-[collapsible=icon]:flex-col group-data-[collapsible=icon]:items-center">
        <ConsoleMenu onToken={() => setTokenOpen(true)} />
        <RailToggle />
      </div>
      <TokenDialog open={tokenOpen} onOpenChange={setTokenOpen} />
    </SidebarFooter>
  )
}

/**
 * The console's own menu: display, language, links, the credential when one is
 * set, and the copyright. It opens upwards from the rail's foot, so it never
 * covers the navigation it sits under.
 */
function ConsoleMenu({ onToken }: { onToken: () => void }) {
  const locale = useLocale()
  const { theme, setTheme } = useTheme()
  const current = theme === 'light' || theme === 'dark' ? theme : 'system'
  const label = t('Console menu')
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={<Button type="button" variant="quiet" size="sm" aria-label={label} title={label}
          className="flex-1 justify-start gap-1.5 px-1.5 text-xs font-normal text-muted-foreground group-data-[collapsible=icon]:w-8 group-data-[collapsible=icon]:flex-none group-data-[collapsible=icon]:justify-center group-data-[collapsible=icon]:px-0" />}>
        <MoreHorizontal className="size-3.5" aria-hidden />
        <span className="group-data-[collapsible=icon]:hidden">{__APP_VERSION__}</span>
      </DropdownMenuTrigger>
      <DropdownMenuContent side="top" align="start" sideOffset={6} className="w-60">
        <DropdownMenuGroup>
          <DropdownMenuLabel>{t('Theme')}</DropdownMenuLabel>
          <DropdownMenuRadioGroup value={current} onValueChange={setTheme}>
            <DropdownMenuRadioItem value="system">{t('Auto')}</DropdownMenuRadioItem>
            <DropdownMenuRadioItem value="light">{t('Light')}</DropdownMenuRadioItem>
            <DropdownMenuRadioItem value="dark">{t('Dark')}</DropdownMenuRadioItem>
          </DropdownMenuRadioGroup>
        </DropdownMenuGroup>
        <DropdownMenuSeparator />
        <DropdownMenuGroup>
          <DropdownMenuLabel>{t('Language')}</DropdownMenuLabel>
          <DropdownMenuRadioGroup value={locale} onValueChange={(next) => setLocale(next as Locale)}>
            <DropdownMenuRadioItem value="en"><span lang="en">English</span></DropdownMenuRadioItem>
            <DropdownMenuRadioItem value="zh-CN"><span lang="zh-CN">中文</span></DropdownMenuRadioItem>
          </DropdownMenuRadioGroup>
        </DropdownMenuGroup>
        <DropdownMenuSeparator />
        <DropdownMenuItem render={<a href="https://github.com/open-linux-router/open-linux-router" target="_blank" rel="noreferrer" />}>
          <GithubMark className="size-4" aria-hidden />GitHub
        </DropdownMenuItem>
        {/* The page carries the list; the menu is the one place it is reachable
            from every route, including the ones the rail does not own. */}
        <DropdownMenuItem render={<Link to="/credits" />}>{t('Credits')}</DropdownMenuItem>
        <TokenMenuItem onOpen={onToken} />
        <DropdownMenuSeparator />
        <div className="px-1.5 py-1 text-xs text-muted-foreground">
          <div>© {new Date().getFullYear()} Open Linux Router</div>
          <div>{__APP_VERSION__}</div>
        </div>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

/** Collapses the rail to its icons; ⌘B / Ctrl+B does the same. */
function RailToggle() {
  const { open, toggleSidebar } = useSidebar()
  const label = open ? t('Collapse sidebar') : t('Expand sidebar')
  return (
    <Button variant="quiet" size="icon-sm" onClick={toggleSidebar} aria-label={label} title={label}
      className="text-muted-foreground group-data-[collapsible=icon]:mx-auto">
      <PanelLeft aria-hidden />
    </Button>
  )
}
