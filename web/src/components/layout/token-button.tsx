import { t, useLocale } from '@/lib/i18n'
import { useQueryClient } from '@tanstack/react-query'
import { KeyRound } from 'lucide-react'
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
import { DropdownMenuItem } from '@/components/ui/dropdown-menu'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { getToken, setToken } from '@/lib/api'

/**
 * Sets the API token the UI sends as a bearer credential.
 *
 * olrd generates the token into /etc/open-linux-router/api-token on first
 * start. There is no user model yet — design.md §10 folds "who may log into
 * olr" into the unbuilt system module — so this is deliberately a credential
 * box and not a login form. It should be replaced wholesale, not grown into
 * one.
 *
 * The phone's header has nowhere to put a menu, so it gets the icon button; the
 * rail's console menu gets the item. Both open the same dialog.
 */

/**
 * Hidden unless a token is actually in use. olrd serves the API without one
 * unless it was started with --auth, which is the common case, and a key icon
 * in the header of a UI that never asked for a credential reads as "you have
 * missed a setup step" — the same confusion that made the raw 401 JSON so
 * bad. The first token is set by AuthGate, which appears on a real 401; these
 * controls exist to change or clear one that is already there.
 */
function inUse() {
  return getToken() !== null
}

export function TokenButton() {
  useLocale()
  const [open, setOpen] = useState(false)
  if (!inUse()) return null
  return (
    <>
      <Button variant="quiet" size="icon" aria-haspopup="dialog" aria-label={t('Set API token')} onClick={() => setOpen(true)}>
        <KeyRound className="size-4" aria-hidden />
      </Button>
      <TokenDialog open={open} onOpenChange={setOpen} />
    </>
  )
}

/**
 * The same dialog, as a row in the rail's console menu.
 *
 * The dialog is the caller's sibling, not this row's child: the menu closes when
 * a row is selected, and anything mounted inside its popup would go with it.
 */
export function TokenMenuItem({ onOpen }: { onOpen: () => void }) {
  useLocale()
  if (!inUse()) return null
  return (
    <DropdownMenuItem onClick={onOpen}>
      <KeyRound aria-hidden />
      {t('API token')}
    </DropdownMenuItem>
  )
}

export function TokenDialog({ open, onOpenChange }: { open: boolean, onOpenChange: (open: boolean) => void }) {
  useLocale()
  const queryClient = useQueryClient()
  const [value, setValue] = useState('')

  function save() {
    setToken(value.trim() || null)
    onOpenChange(false)
    // Every query failed for the same reason; refetch them all rather than
    // making the operator reload the page.
    queryClient.invalidateQueries()
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        onOpenChange(next)
        if (next) setValue(getToken() ?? '')
      }}
    >
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t('API token')}</DialogTitle>
          <DialogDescription>
            Sent as a bearer token with every request. olrd writes it to
            <code className="mx-1 rounded bg-muted px-1 py-0.5 text-xs">
              /etc/open-linux-router/api-token
            </code>
            on first start. Not needed over the unix socket, which is
            authenticated by its file permissions.
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-2">
          <Label htmlFor="api-token">{t('Token')}</Label>
          <Input
            id="api-token"
            type="password"
            autoComplete="off"
            spellCheck={false}
            value={value}
            onChange={(e) => setValue(e.target.value)}
            onKeyDown={(e) => e.key === 'Enter' && save()}
          />
        </div>

        <DialogFooter>
          <Button variant="quiet" onClick={() => onOpenChange(false)}>{t('Cancel')}</Button>
          <Button onClick={save}>{t('Save')}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
