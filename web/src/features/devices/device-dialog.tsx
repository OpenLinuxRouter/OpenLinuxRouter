import { t, useLocale } from '@/lib/i18n'
import { Trash2 } from 'lucide-react'
import { useState } from 'react'

import { Badge } from '@/components/ui/badge'
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

import { adapterLabel, idFromAddress, roleLabels } from './device'
import type { Adapter, DeviceRole, Device } from './types'

/** The fields a person actually types; adapter and roles come from the type. */
export interface DeviceDraft {
  id: string
  name: string
  adapter: Adapter
  roles: DeviceRole[]
  address: string
  username: string
  password: string
}

/** One shape this page can create, with a label the caller has translated. */
export interface DeviceType {
  label: string
  adapter: Adapter
  roles: DeviceRole[]
}

const EMPTY = { id: '', name: '', address: '', username: '', password: '' }

/** OpenWrt signs in as root; Mercury's account is whatever its admin set. */
function defaultUsername(adapter: Adapter): string {
  return adapter === 'openwrt' ? 'root' : ''
}

/**
 * Adds one box to the device inventory, or edits the one that is open.
 *
 * The inventory is shared: an access point and a switch are the same kind of
 * entry, so this dialog is the same form and only its type list and its nouns
 * differ. The identifier cannot change on an edit — the API takes it from the
 * path, so a rename would store a second entry and leave the first one behind.
 *
 * A stored password comes back masked. Leaving the mask in place and saving
 * means "unchanged", which is the only honest reading of a form nobody retyped;
 * clearing the field is how an operator deliberately drops it.
 */
export function DeviceDialog({
  open,
  onOpenChange,
  initial,
  busy,
  existing = [],
  types = [],
  subject = 'equipment',
  onSubmit,
  onRemove,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  initial?: Device
  busy: boolean
  /** Client IDs already in the inventory, so adding cannot silently replace one. */
  existing?: string[]
  /** The shapes this page can create. Empty when only an existing entry is edited. */
  types?: DeviceType[]
  subject?: 'equipment' | 'access point'
  onSubmit: (draft: DeviceDraft) => void
  onRemove?: () => void
}) {
  useLocale()
  const editing = initial !== undefined
  const copy = COPY[subject]
  const [draft, setDraft] = useState(() =>
    initial
      ? {
          id: initial.id,
          name: initial.name ?? '',
          address: initial.address,
          username: initial.username ?? '',
          password: initial.password ?? '',
        }
      : { ...EMPTY, username: defaultUsername(types[0]?.adapter ?? 'openwrt') },
  )
  const [typeIndex, setTypeIndex] = useState(0)
  const [confirmRemove, setConfirmRemove] = useState(false)

  const type = types[typeIndex] ?? types[0]
  const adapter = initial?.adapter ?? type?.adapter ?? 'openwrt'
  const roles = initial ? (initial.roles ?? []) : (type?.roles ?? [])

  function set<K extends keyof typeof EMPTY>(key: K, value: string) {
    setDraft(current => ({ ...current, [key]: value }))
  }

  const id = draft.id.trim() || idFromAddress(draft.address)
  const conflict = !editing && id !== '' && existing.includes(id)
  const canSave = draft.address.trim() !== '' && id !== '' && !conflict && !busy

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{editing ? t(copy.editTitle) : t(copy.addTitle)}</DialogTitle>
          <DialogDescription>{t(copy.description)}</DialogDescription>
        </DialogHeader>

        <div className="space-y-4">
          {initial ? (
            <Field label={t('Type')} hint={t('Fixed when the entry was added. Editing changes its address and credential.')}>
              <div className="flex flex-wrap items-center gap-1.5">
                <Badge variant="secondary">{t(adapterLabel(adapter))}</Badge>
                {roleLabels({ id: initial.id, adapter, address: initial.address, roles }).map(role => (
                  <Badge key={role} variant="outline">{t(role)}</Badge>
                ))}
              </div>
            </Field>
          ) : types.length > 1 ? (
            <Field label={t('Type')} hint={t('Decides the adapter olr speaks and the pages that apply to this box.')} htmlFor="device-type">
              <select
                id="device-type"
                value={typeIndex}
                onChange={event => {
                  const next = types[Number(event.target.value)]
                  if (next) {
                    // A default the operator has not retyped follows the type;
                    // a username they chose stays theirs.
                    const previous = defaultUsername(adapter)
                    setDraft(current => ({ ...current, username: current.username === previous ? defaultUsername(next.adapter) : current.username }))
                  }
                  setTypeIndex(Number(event.target.value))
                }}
                className="h-9 w-full min-w-0 rounded-lg border border-input bg-card px-2.5 text-sm text-foreground transition-colors outline-none focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring/20 dark:bg-input/30"
              >
                {types.map((option, index) => <option key={option.label} value={index}>{option.label}</option>)}
              </select>
            </Field>
          ) : null}

          <Field label={t('Address')} hint={t('Management origin only, for example http://192.168.1.2. No path or query.')} htmlFor="device-address">
            <Input
              id="device-address"
              value={draft.address}
              autoComplete="off"
              placeholder="http://192.168.1.2"
              onChange={event => set('address', event.target.value)}
              onBlur={() => { if (!editing && !draft.id.trim()) set('id', idFromAddress(draft.address)) }}
            />
          </Field>

          <Field label={t('Name')} hint={t('Shown in the page header. Optional.')} htmlFor="device-name">
            <Input id="device-name" value={draft.name} autoComplete="off" onChange={event => set('name', event.target.value)} />
          </Field>

          {!editing && (
            <Field label={t('Identifier')} hint={t('Unique in the inventory: letters, digits, dashes and underscores. Derived from the address when left empty.')} htmlFor="device-id">
              <Input
                id="device-id"
                value={draft.id}
                autoComplete="off"
                placeholder={idFromAddress(draft.address)}
                aria-invalid={conflict}
                onChange={event => set('id', event.target.value)}
              />
              {conflict && <p className="text-xs leading-4 text-destructive">{t(copy.conflict)}</p>}
            </Field>
          )}

          <div className="grid gap-4 sm:grid-cols-2">
            <Field label={t('Username')} hint={t(copy.usernameHint)} htmlFor="device-username">
              <Input id="device-username" value={draft.username} autoComplete="username" onChange={event => set('username', event.target.value)} />
            </Field>
            <Field
              label={t('Password')}
              hint={editing ? t('Leave the mask to keep the saved password; clear the field to drop it.') : t('The management password. It is stored in this router\'s configuration.')}
              htmlFor="device-password"
            >
              <Input
                id="device-password"
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
                <span className="text-xs text-muted-foreground">{t(copy.forget)}</span>
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
            onClick={() => onSubmit({ ...draft, id, adapter, roles, name: draft.name.trim(), address: draft.address.trim(), username: draft.username.trim() })}
            disabled={!canSave}
          >
            {busy ? t('Saving…') : t('Save')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

const COPY = {
  equipment: {
    addTitle: 'Add a device',
    editTitle: 'Device settings',
    description: 'olr reads this device over its management interface. It never changes it from this page.',
    conflict: 'Another entry already uses this identifier.',
    forget: 'Forget this device?',
    usernameHint: 'The account olr signs in with. OpenWrt defaults to root; Mercury uses its admin account.',
  },
  'access point': {
    addTitle: 'Add an access point',
    editTitle: 'Access point settings',
    description: 'olr reads this access point over its management interface. It never changes radios, channels or clients from this page.',
    conflict: 'An access point already uses this identifier.',
    forget: 'Forget this access point?',
    usernameHint: 'OpenWrt defaults to root.',
  },
} as const

function Field({ label, hint, htmlFor, children }: {
  label: string
  hint?: string
  htmlFor?: string
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
