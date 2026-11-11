import { t, useLocale } from '@/lib/i18n'
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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { groupOptions, MAX_GROUP_DEPTH } from '@/features/clients/group-tree'
import { describeError } from '@/features/clients/queries'
import type { ClientsGroup } from '@/lib/config-types'

/** internal/clients/config.go MaxGroupNameLen. The server is still the judge. */
const MAX_GROUP_NAME = 32

/** The parent Select's value for "at the top", which no trimmed name can be. */
const TOP = ' top'

/**
 * The one form both creating and renaming use: a name.
 *
 * Errors from the server are shown under the field rather than in a toast.
 * The ones worth having — "there is already a group called IoT", "name is 40
 * characters; the limit is 32" — are about what was just typed, and a toast
 * would disappear while the operator was still looking at the field to fix.
 */
export function GroupNameDialog({
  from,
  parent: initialParent = '',
  groups = [],
  busy,
  onOpenChange,
  onSubmit,
}: {
  /** Undefined when creating. */
  from?: string
  /** Where a new group goes, preselected; empty for the top. */
  parent?: string
  /** Every group, for the parent picker. Only asked for when creating. */
  groups?: ClientsGroup[]
  busy: boolean
  onOpenChange: (open: boolean) => void
  onSubmit: (name: string, parent: string) => Promise<void>
}) {
  useLocale()
  const [name, setName] = useState(from ?? '')
  const [parent, setParent] = useState(initialParent)
  const [error, setError] = useState<string | null>(null)
  const trimmed = name.trim()
  const tooLong = [...trimmed].length > MAX_GROUP_NAME

  // Only where a new group can go: a parent already at the deepest level
  // would make a group the server refuses. Moving an existing group is its own
  // menu, so renaming never shows this at all.
  const parents = from === undefined ? groupOptions(groups).filter((g) => g.depth < MAX_GROUP_DEPTH - 1) : []
  const depthOf = new Map(parents.map((g) => [g.name, g.depth]))

  async function submit() {
    setError(null)
    try {
      await onSubmit(trimmed, parent)
    } catch (e) {
      setError(describeError(e))
    }
  }

  return (
    <Dialog open onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <form
          className="grid gap-4"
          onSubmit={(e) => {
            e.preventDefault()
            if (trimmed && !tooLong) void submit()
          }}
        >
          <DialogHeader>
            <DialogTitle>{from === undefined ? 'New group' : `Rename ${from}`}</DialogTitle>
            <DialogDescription>
              {from === undefined
                ? 'A set of your devices — Serving, Personal, IoT, whatever makes sense to you. It changes how the overview is laid out, and nothing about how anything connects.'
                : 'Every device in it moves with it.'}
            </DialogDescription>
          </DialogHeader>

          <div className="grid gap-2">
            <Label htmlFor="group-name">{t('Name')}</Label>
            <Input
              id="group-name"
              autoFocus
              value={name}
              placeholder={t('Personal')}
              aria-invalid={Boolean(error) || tooLong}
              onChange={(e) => {
                setName(e.target.value)
                setError(null)
              }}
            />
            {tooLong ? (
              <p className="text-xs text-destructive"> {t('Keep it to')} {MAX_GROUP_NAME} characters or fewer.
              </p>
            ) : error ? (
              <p className="text-xs text-destructive">{error}</p>
            ) : null}
          </div>

          {parents.length > 0 && (
            <div className="grid gap-2">
              <Label htmlFor="group-parent">{t('Inside')}</Label>
              <Select value={parent || TOP} onValueChange={(v) => setParent(!v || v === TOP ? '' : v)}>
                <SelectTrigger id="group-parent" className="w-full">
                  <SelectValue>
                    {(value: string) => (value === TOP || !value ? t('Nothing — a top-level group') : value)}
                  </SelectValue>
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value={TOP}>{t('Nothing — a top-level group')}</SelectItem>
                  {parents.map((g) => (
                    <SelectItem key={g.name} value={g.name}>
                      {/* Indented by depth, so a subgroup reads as one. */}
                      <span style={{ paddingLeft: (depthOf.get(g.name) ?? 0) * 12 }}>{g.name}</span>
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          )}

          <DialogFooter>
            <Button type="button" variant="quiet" onClick={() => onOpenChange(false)}>{t('Cancel')}</Button>
            <Button type="submit" disabled={busy || !trimmed || tooLong}>
              {from === undefined ? 'Create group' : t('Rename')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
