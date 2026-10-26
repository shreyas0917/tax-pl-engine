import { AlertCircle, RotateCw } from 'lucide-react'
import type { ReactNode } from 'react'
import { Button } from './button'
import { cn } from '@/lib/utils'

export function EmptyState({ title, body, action, className }: { title: string; body?: ReactNode; action?: ReactNode; className?: string }) {
  return (
    <div className={cn('flex flex-col items-start gap-3 px-6 py-10 sm:px-8', className)}>
      <div>
        <h3 className="text-base font-semibold">{title}</h3>
        {body && <p className="mt-1 max-w-prose text-sm text-muted">{body}</p>}
      </div>
      {action}
    </div>
  )
}

export function ErrorState({ title = 'Could not load this', message, onRetry, className }: { title?: string; message?: string; onRetry?: () => void; className?: string }) {
  return (
    <div role="alert" className={cn('flex flex-col items-start gap-3 px-6 py-8 sm:px-8', className)}>
      <div className="flex items-start gap-2">
        <AlertCircle className="mt-0.5 size-4 shrink-0 text-loss" aria-hidden />
        <div>
          <h3 className="text-base font-semibold">{title}</h3>
          {message && <p className="mt-1 max-w-prose text-sm text-muted">{message}</p>}
        </div>
      </div>
      {onRetry && (
        <Button variant="secondary" size="sm" onClick={onRetry}>
          <RotateCw aria-hidden /> Try again
        </Button>
      )}
    </div>
  )
}

export function InlineError({ message }: { message: string | null | undefined }) {
  if (!message) return null
  return (
    <p role="alert" className="flex items-start gap-2 rounded-md border border-loss/40 bg-loss-soft px-3 py-2 text-sm text-loss">
      <AlertCircle className="mt-0.5 size-4 shrink-0" aria-hidden />
      <span>{message}</span>
    </p>
  )
}
