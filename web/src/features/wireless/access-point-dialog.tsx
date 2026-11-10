import { t, useLocale } from '@/lib/i18n'
import { Trash2 } from 'lucide-react'
import { useState } from 'react'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

import type { NetworkDevice } from './types'
import { idFromAddress } from './wireless'

/** The fields a person actually types; everything else is fixed by this page. */
export interface AccessPointDraft {
  id: string
  name: string
  address: string
  username: string
  password: string
}

const EMPTY: AccessPointDraft = { id: '', name: '', address: '', username: 'root', password: '' }

/**
 * Adds one access point to the equipment inventory, or edits the selected one.
 *
 * The identifier cannot change on an edit: the API takes it from the path, so a
 * rename would store a second entry and leave the first one behind. Removing a
 * device only forgets it here — nothing on the access point is touched.
 *
 * A stored password comes back masked. Leaving the mask in place and saving
 * means "unchanged", which is the only honest reading of a form nobody retyped;
 * clearing the field is how an operator deliberately drops it.
 */
export function AccessPointDialog({
  open,
  onOpenChange,
  initial,
  busy,
  existing = [],
  onSubmit,
  onRemove,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  initial?: NetworkDevice
  busy: boolean
  /** Device IDs already in the inventory, so adding cannot silently replace one. */
  existing?: string[]
  onSubmit: (draft: AccessPointDraft) => void
  onRemove?: () => void
}) {
  useLocale()
  const editing = initial !== undefined
  const [draft, setDraft] = useState<AccessPointDraft>(() =>
    initial
      ? {
          id: initial.id,
          name: initial.name ?? '',
          address: initial.address,
          username: initial.username ?? '',
          password: initial.password ?? '',
        }
      : EMPTY,
  )
  const [confirmRemove, setConfirmRemove] = useState(false)

  function set<K extends keyof AccessPointDraft>(key: K, value: AccessPointDraft[K]) {
    setDraft(current => ({ ...current, [key]: value }))
  }

  const id = draft.id.trim() || idFromAddress(draft.address)
  const conflict = !editing && id !== '' && existing.includes(id)
  const canSave = draft.address.trim() !== '' && id !== '' && !conflict && !busy

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{editing ? t('Access point settings') : t('Add an access point')}</DialogTitle>
          <DialogDescription>
            {t('olr reads this access point over its management interface. It never changes radios, channels or clients from this page.')}
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4">
          <Field label={t('Address')} hint={t('Management origin only, for example http://192.168.1.2. No path or query.')} htmlFor="ap-address">
            <Input
              id="ap-address"
              value={draft.address}
              autoComplete="off"
              placeholder="http://192.168.1.2"
              onChange={event => set('address', event.target.value)}
              onBlur={() => { if (!editing && !draft.id.trim()) set('id', idFromAddress(draft.address)) }}
            />
          </Field>

          <Field label={t('Name')} hint={t('Shown in the page header. Optional.')} htmlFor="ap-name">
            <Input id="ap-name" value={draft.name} autoComplete="off" onChange={event => set('name', event.target.value)} />
          </Field>

          {!editing && (
            <Field label={t('Identifier')} hint={t('Unique in the inventory: letters, digits, dashes and underscores.')} htmlFor="ap-id">
              <Input
                id="ap-id"
                value={draft.id}
                autoComplete="off"
                placeholder={idFromAddress(draft.address)}
                aria-invalid={conflict}
                onChange={event => set('id', event.target.value)}
              />
              {conflict && <p className="text-xs leading-4 text-destructive">{t('An access point already uses this identifier.')}</p>}
            </Field>
          )}

          <div className="grid gap-4 sm:grid-cols-2">
            <Field label={t('Username')} hint={t('OpenWrt defaults to root.')} htmlFor="ap-username">
              <Input id="ap-username" value={draft.username} autoComplete="username" onChange={event => set('username', event.target.value)} />
            </Field>
            <Field
              label={t('Password')}
              hint={editing ? t('Leave the mask to keep the saved password; clear the field to drop it.') : t('The management password. It is stored in this router\'s configuration.')}
              htmlFor="ap-password"
            >
              <Input
                id="ap-password"
                type="password"
                value={draft.password}
                autoComplete="new-password"
                onChange={event => set('password', event.target.value)}
              />
            </Field>
          </div>
        </div>

        <DialogFooter>
          {editing && onRemove && (
            confirmRemove ? (
              <div className="flex items-center gap-2 sm:mr-auto">
                <span className="text-xs text-muted-foreground">{t('Forget this access point?')}</span>
                <Button variant="danger" size="sm" onClick={onRemove} disabled={busy}>{t('Forget')}</Button>
              </div>
            ) : (
              <Button variant="danger" size="sm" className="sm:mr-auto" onClick={() => setConfirmRemove(true)} disabled={busy}>
                <Trash2 />
                {t('Forget')}
              </Button>
            )
          )}
          <Button variant="secondary" onClick={() => onOpenChange(false)}>{t('Cancel')}</Button>
          <Button
            onClick={() => onSubmit({ ...draft, id, name: draft.name.trim(), address: draft.address.trim(), username: draft.username.trim() })}
            disabled={!canSave}
          >
            {busy ? t('Saving…') : t('Save')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function Field({ label, hint, htmlFor, children }: {
  label: string
  hint?: string
  htmlFor: string
  children: React.ReactNode
}) {
  return (
    <div className="space-y-1.5">
      <Label htmlFor={htmlFor}>{label}</Label>
      {children}
      {hint && <p className="text-xs leading-4 text-muted-foreground">{hint}</p>}
    </div>
  )
}
