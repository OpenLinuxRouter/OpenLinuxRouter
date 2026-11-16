import type { ComponentProps, ReactNode } from 'react'

import { cn } from '@/lib/utils'

/** Use inside a dl. Row density never shrinks a phone's touch target. */
export function PropertyRow({ label, children, size = 'md', className, ...props }: ComponentProps<'div'> & {
  label: ReactNode
  size?: 'sm' | 'md'
}) {
  return <div data-slot="property-row" data-size={size} className={cn(
    'grid min-h-11 grid-cols-[6rem_minmax(0,1fr)] items-center gap-3 py-1 text-sm',
    size === 'sm' ? 'sm:min-h-8' : 'sm:min-h-9', className,
  )} {...props}>
    <dt className="text-muted-foreground">{label}</dt>
    <dd className="min-w-0 text-right text-foreground [overflow-wrap:anywhere]">{children}</dd>
  </div>
}

export function TechnicalValue({ className, ...props }: ComponentProps<'span'>) {
  return <span data-slot="technical-value" className={cn('font-mono text-meta leading-5 text-foreground [overflow-wrap:anywhere]', className)} {...props} />
}
