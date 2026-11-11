import { t, useLocale } from '@/lib/i18n'
import { AlertTriangle, Check, X } from 'lucide-react'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Disclosure } from '@/components/ui/disclosure'
import { Skeleton } from '@/components/ui/skeleton'
import { PlanDiff, PlanReasons, impactHint } from '@/features/dhcp/impact'
import { useDhcpConfig } from '@/features/dhcp/queries'
import { useDhcpApply } from '@/features/dhcp/use-apply'
import type { DhcpConfig } from '@/lib/config-types'

/** The dns module's editor, for dhcp. See features/dns/editor.tsx for why. */
export function useDhcpEditor() {
  useLocale()
  const config = useDhcpConfig()
  const applier = useDhcpApply()

  return {
    config: config.data,
    busy: applier.busy,
    change: (next: DhcpConfig) => applier.submit(next),
    applier,
    gate: config.isPending ? (
      <PageSkeleton />
    ) : config.isError ? (
      <Alert variant="destructive">
        <AlertTriangle />
        <AlertTitle>{t('Could not load the address settings')}</AlertTitle>
        <AlertDescription>{(config.error as Error).message}</AlertDescription>
      </Alert>
    ) : null,
  }
}

export function ApplyOutcome({ applier }: { applier: ReturnType<typeof useDhcpApply> }) {
  useLocale()
  return (
    <>
      {applier.failure && (
        <Alert variant="destructive" role="alert">
          <AlertTriangle />
          <AlertTitle>{t('Only part of the change went through')}</AlertTitle>
          <AlertDescription className="space-y-3">
            <p>{t('The steps that finished have already taken effect and will not be undone. Fixing the cause and applying again is safe — it picks up where this left off.')}</p>
            <Disclosure summary={t('Which steps ran')}>
              <ul className="space-y-1 pt-1 font-mono text-xs">
                {applier.failure.steps?.map((step) => (
                  <li key={step.description} className="flex gap-2">
                    {step.done ? (
                      <Check className="mt-0.5 size-3.5 shrink-0 text-success-foreground" aria-label={t('done')} />
                    ) : (
                      <X className="mt-0.5 size-3.5 shrink-0 text-destructive" aria-label={t('failed')} />
                    )}
                    <span>
                      {step.description}
                      {step.error && <span className="block opacity-80">{step.error}</span>}
                    </span>
                  </li>
                ))}
              </ul>
            </Disclosure>
            <Button size="sm" variant="secondary" onClick={applier.dismissFailure}>{t('Dismiss')}</Button>
          </AlertDescription>
        </Alert>
      )}

      <ConfirmDialog applier={applier} />
    </>
  )
}

function ConfirmDialog({ applier }: { applier: ReturnType<typeof useDhcpApply> }) {
  useLocale()
  const pending = applier.confirming

  return (
    <Dialog open={pending !== null} onOpenChange={(open) => !open && applier.cancel()}>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <AlertTriangle className="size-4 text-destructive" aria-hidden />{t('This will disconnect clients')}</DialogTitle>
          <DialogDescription>
            {pending && impactHint(pending.plan.impact)} Every other change
            applies the moment you make it — this one asks first.
          </DialogDescription>
        </DialogHeader>

        {/* The reasons stay in front of the operator; the diff is the evidence
            behind them. design.md §6.4 needs the diff to stay inspectable, so
            it is one click away rather than the first thing in the dialog. */}
        {pending && (
          <div className="space-y-2">
            <PlanReasons plan={pending.plan} />
            <Disclosure summary={t('Show exactly what will change')}>
              <div className="max-h-[45vh] overflow-auto pt-2">
                <PlanDiff plan={pending.plan} />
              </div>
            </Disclosure>
          </div>
        )}

        <DialogFooter>
          {/* No impact badge here: the title already says "This will disconnect
              devices", and the only plan that reaches this dialog is the
              disruptive one. The per-file badges inside the diff still earn
              their place, because a change can span files of mixed impact. */}
          <Button variant="quiet" onClick={applier.cancel}>{t('Cancel')}</Button>
          <Button onClick={applier.confirm}>{t('Apply anyway')}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function PageSkeleton() {
  useLocale()
  return (
    <div className="space-y-6">
      <Skeleton className="h-24 w-full rounded-xl" />
      <Skeleton className="h-56 w-full rounded-xl" />
    </div>
  )
}
