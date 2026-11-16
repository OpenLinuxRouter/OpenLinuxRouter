import { t, useLocale } from '@/lib/i18n'
import { useEffect, useState } from 'react'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'

/**
 * A text field that saves on a button rather than as you type.
 *
 * design.md §5.1 wants the GUI to apply instantly, and switches do. Free text
 * cannot: every keystroke of "192.168.1.1:53" is an invalid address, so applying
 * per character would mean a plan request per keystroke and a toast storm of
 * validation errors on the way to a correct value.
 *
 * Shared rather than copied now that the dns and dhcp sections are a page each
 * per setting — it was written twice before and would be a dozen times over.
 */
export function EditableField({
  id,
  label,
  hint,
  placeholder,
  stored,
  onSave,
  busy,
  multiline,
}: {
  id: string
  label?: string
  hint?: React.ReactNode
  placeholder?: string
  stored: string
  onSave: (value: string) => void
  busy: boolean
  multiline?: boolean
}) {
  useLocale()
  const [draft, setDraft] = useState(stored)

  // Re-sync when the value changes underneath — after an apply lands, or when
  // another client changed it and the poll noticed.
  useEffect(() => setDraft(stored), [stored])

  const dirty = draft !== stored

  return (
    <div className="grid max-w-[45rem] gap-2">
      {label && <Label htmlFor={id}>{label}</Label>}
      {multiline ? (
        <Textarea
          id={id}
          className="min-h-36 font-mono text-meta leading-relaxed"
          spellCheck={false}
          placeholder={placeholder}
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
        />
      ) : (
        <Input
          id={id}
          // Bounded, unlike the textarea below it: a single address in a field
          // as wide as a 27" display is a target you have to aim at, and the
          // value it holds is never longer than this.
          className="w-full"
          placeholder={placeholder}
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
        />
      )}
      {hint && <p className="max-w-prose text-meta leading-relaxed text-muted-foreground">{hint}</p>}
      {dirty && (
        <div className="flex flex-wrap items-center justify-end gap-2 pt-1">
          <Button variant="quiet" size="sm" onClick={() => setDraft(stored)}> {t('Revert')} </Button>
          <Button size="sm" disabled={busy} onClick={() => onSave(draft)}>{t('Save')}</Button>
        </div>
      )}
    </div>
  )
}

/** Splits a comma-separated field, dropping blanks, so an empty one is omitted. */
export function splitList(value: string): string[] | undefined {
  const items = value
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean)
  return items.length ? items : undefined
}

/**
 * A switch with its own explanation, for a setting that is a sentence rather
 * than a field. The label carries the consequence; the paragraph carries why it
 * is worth having.
 */
export function SwitchField({
  id,
  label,
  hint,
  checked,
  busy,
  onChange,
  variant = 'panel',
}: {
  id: string
  label: string
  hint: React.ReactNode
  checked: boolean
  busy: boolean
  onChange: (on: boolean) => void
  /**
   * `panel` is a setting that owns a surface of its own. `inline` sits on a
   * line inside a card that already has one, and keeps the label next to the
   * switch instead of putting the switch alone at the edge.
   */
  variant?: 'panel' | 'inline'
}) {
  useLocale()
  return (
    <div className={variant === 'panel'
      ? 'panel-surface flex items-center gap-6 p-4 sm:p-6'
      : 'flex min-h-11 min-w-0 flex-1 items-center gap-6'}>
      <div className="min-w-0 flex-1">
        <Label htmlFor={id} className="text-sm font-medium">{label}</Label>
        <p className="max-w-prose text-xs text-muted-foreground">{hint}</p>
      </div>
      <Switch
        id={id}
        aria-label={label}
        checked={checked}
        busy={busy}
        onCheckedChange={onChange}
      />
    </div>
  )
}
