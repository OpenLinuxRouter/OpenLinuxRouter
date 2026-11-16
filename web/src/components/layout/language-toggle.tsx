import { Languages } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { setLocale, t, useLocale, type Locale } from '@/lib/i18n'
import { cn } from '@/lib/utils'

/** The language's own writing, for the word the control shows. */
const SELF: Record<Locale, string> = { en: 'English', 'zh-CN': '中文' }

/** Its name in each interface language, for the tooltip and accessible label. */
const NAMES: Record<Locale, Record<Locale, string>> = {
  en: { en: 'English', 'zh-CN': 'Chinese' },
  'zh-CN': { en: '英文', 'zh-CN': '中文' },
}

/**
 * The current language in its own writing, as footer text: no icon and no
 * separate "Language" label, because the word is the language. It reads as the
 * state it reports, like the theme control beside it, which shows the theme in
 * use, and its tooltip names both languages so a click is still announced. Being
 * always the language of the interface, the word is set in the same script as
 * the rest of the footer and shares its line with it. The 8px side padding makes
 * the target reach into the gap between items; the footer cancels it so the
 * visible gap stays 16px.
 *
 * The rail's icon state is the one place the word cannot follow: 32px cannot set
 * "English", so there the control becomes the one icon that means the same
 * thing, and the tooltip still names both languages.
 */
export function LanguageToggle({ className }: { className?: string }) {
  const locale = useLocale()
  const next: Locale = locale === 'en' ? 'zh-CN' : 'en'
  const label = t('Language: {current}. Switch to {next}', {
    current: NAMES[locale][locale],
    next: NAMES[locale][next],
  })
  return (
    <Button variant="quiet" size="sm" className={cn('min-h-11 px-2 text-xs font-normal text-muted-foreground transition-colors duration-150 hover:bg-transparent hover:text-foreground active:bg-transparent dark:hover:bg-transparent lg:items-end', className)}
      aria-label={label} title={label}
      onClick={() => setLocale(next)}>
      <span lang={locale} className="group-data-[collapsible=icon]:hidden">{SELF[locale]}</span>
      <Languages className="hidden size-3.5 group-data-[collapsible=icon]:block" aria-hidden />
    </Button>
  )
}
