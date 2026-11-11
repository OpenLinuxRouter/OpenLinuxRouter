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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import type { BlockedNameResponse, Policy } from '@/lib/config-types'

// The vocabulary comes from the published schema (BlockResponse.JSONSchema in
// Go), so only the labels are added here — the values are not restated.
//
// Both options are phrased as what the device experiences, because that is the
// thing an operator is actually choosing between. "NXDOMAIN" is not.
const RESPONSE_OPTIONS: {
  value: Exclude<BlockedNameResponse, ''>
  label: string
  hint: string
}[] = [
  {
    value: 'nxdomain',
    get label() { return t('Say it doesn\'t exist') },
    hint: 'The honest answer. Clients cache it and stop asking. Recommended.',
  },
  {
    value: 'zero',
    get label() { return t('Send it nowhere') },
    hint: 'Fails instantly instead. Use when an app treats the first option as "no internet" and retries forever.',
  },
]

const RESPONSE_LABEL = new Map(RESPONSE_OPTIONS.map((o) => [o.value as string, o.label]))

const EMPTY: Policy = { name: '' }

export function PolicyDialog({
  open,
  onOpenChange,
  initial,
  onSubmit,
  onRemove,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** Undefined when adding. */
  initial?: Policy
  onSubmit: (policy: Policy) => void
  onRemove?: () => void
}) {
  useLocale()
  const [draft, setDraft] = useState<Policy>(initial ?? EMPTY)
  const editing = initial !== undefined

  function field<K extends keyof Policy>(key: K, value: Policy[K]) {
    setDraft((d) => ({ ...d, [key]: value }))
  }

  // Only what the server requires. Whether a name is well-formed, whether two
  // policies claim the same clients, whether there is more than one default —
  // all of that is the server's job (design.md §5.3.1) and is deliberately not
  // duplicated here.
  const complete = draft.name.trim() !== ''

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        onOpenChange(next)
        if (next) setDraft(initial ?? EMPTY)
      }}
    >
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{editing ? 'Edit rule' : t('Add rule')}</DialogTitle>
          <DialogDescription>
            What a set of devices is allowed to look up. Leave the devices box
            empty to make this the rule for everyone not covered by another.
          </DialogDescription>
        </DialogHeader>

        <div className="grid gap-4">
          <div className="grid gap-2">
            <Label htmlFor="policy-name">{t('Name')}</Label>
            <Input
              id="policy-name"
              placeholder="kids"
              value={draft.name}
              disabled={editing}
              onChange={(e) => field('name', e.target.value)}
            />
            {editing && (
              <p className="text-xs text-muted-foreground">
                The name identifies this rule and cannot be changed. Remove and
                re-add to rename it.
              </p>
            )}
          </div>

          <div className="grid gap-2">
            <Label htmlFor="policy-clients">{t('Clients')}</Label>
            <Input
              id="policy-clients"
              placeholder="everyone not covered by another rule"
              value={(draft.clients ?? []).join(', ')}
              onChange={(e) => field('clients', splitList(e.target.value))}
            />
            <p className="text-xs text-muted-foreground">
              Addresses or ranges, comma separated — 192.168.1.50/32 for one
              device, 192.168.20.0/24 for a whole network. The most specific
              match wins, so a rule for one tablet beats a rule for the house.
            </p>
          </div>

          <div className="grid gap-2">
            <Label htmlFor="policy-block">{t('Blocked')}</Label>
            <Textarea
              id="policy-block"
              className="min-h-24 font-mono text-xs"
              spellCheck={false}
              placeholder={'ads.example.com\ntracker.example.net'}
              value={(draft.block ?? []).join('\n')}
              onChange={(e) => field('block', splitLines(e.target.value))}
            />
            <p className="text-xs text-muted-foreground">
              One name per line. Each covers everything beneath it, so
              example.com also blocks www.example.com.
            </p>
          </div>

          <div className="grid gap-2">
            <Label htmlFor="policy-allow">{t('Exceptions')}</Label>
            <Textarea
              id="policy-allow"
              className="min-h-16 font-mono text-xs"
              spellCheck={false}
              placeholder="classroom.example.com"
              value={(draft.allow ?? []).join('\n')}
              onChange={(e) => field('allow', splitLines(e.target.value))}
            />
            <p className="text-xs text-muted-foreground">
              Beat the blocked list, so you can block a whole site except the one
              part you need.
            </p>
          </div>

          <div className="grid gap-2">
            <Label htmlFor="policy-response">{t('When something is blocked')}</Label>
            <Select
              value={draft.response || 'nxdomain'}
              onValueChange={(v) => field('response', v as BlockedNameResponse)}
            >
              <SelectTrigger id="policy-response" className="w-full">
                {/* Without the render function the trigger shows the raw schema
                    value, which here would read "nxdomain". */}
                <SelectValue>
                  {(value: string) => RESPONSE_LABEL.get(value) ?? RESPONSE_OPTIONS[0].label}
                </SelectValue>
              </SelectTrigger>
              <SelectContent>
                {RESPONSE_OPTIONS.map(({ value, label, hint }) => (
                  <SelectItem key={value} value={value}>
                    <span className="flex flex-col gap-0.5">
                      <span>{label}</span>
                      <span className="text-xs text-muted-foreground">{hint}</span>
                    </span>
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        </div>

        <DialogFooter>
          {onRemove && (
            <Button
              variant="danger"
              className="mr-auto"
              onClick={() => {
                onRemove()
                onOpenChange(false)
              }}
            >
              <Trash2 className="size-4" aria-hidden />{t('Remove')}</Button>
          )}
          <Button variant="quiet" onClick={() => onOpenChange(false)}>{t('Cancel')}</Button>
          <Button
            disabled={!complete}
            onClick={() => {
              onSubmit(normalise(draft))
              onOpenChange(false)
            }}
          >
            {editing ? t('Save') : t('Add')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function splitList(value: string): string[] | undefined {
  const items = value
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean)
  return items.length ? items : undefined
}

/** One per line, which is how a blocklist is actually pasted. */
function splitLines(value: string): string[] | undefined {
  const items = value
    .split('\n')
    .map((s) => s.trim())
    .filter(Boolean)
  return items.length ? items : undefined
}

/** Trims, and drops empty optional fields so they are omitted rather than sent blank. */
function normalise(policy: Policy): Policy {
  return {
    ...policy,
    name: policy.name.trim(),
    response: policy.response || undefined,
  }
}
