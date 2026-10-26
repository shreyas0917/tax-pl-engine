import type { ReactNode } from 'react'
import { cn } from '@/lib/utils'

export function PageHeader({ title, lede, actions, className }: { title: string; lede?: ReactNode; actions?: ReactNode; className?: string }) {
  return (
    <div className={cn('mb-6 flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between', className)}>
      <div className="min-w-0">
        <h1 className="text-2xl font-semibold">{title}</h1>
        {lede && <p className="mt-1 max-w-prose text-sm text-muted">{lede}</p>}
      </div>
      {actions && <div className="flex shrink-0 items-center gap-2">{actions}</div>}
    </div>
  )
}

export function Section({ title, children, className, aside }: { title?: string; children: ReactNode; className?: string; aside?: ReactNode }) {
  return (
    <section className={cn('panel overflow-hidden', className)}>
      {title && (
        <div className="flex items-center justify-between gap-3 border-b border-rule px-4 py-3 sm:px-5">
          <h2 className="text-[15px] font-semibold">{title}</h2>
          {aside}
        </div>
      )}
      {children}
    </section>
  )
}
