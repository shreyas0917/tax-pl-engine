import type { HTMLAttributes } from 'react'
import { cn } from '@/lib/utils'

export function Skeleton({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return <div aria-hidden className={cn('skeleton h-4 w-full', className)} {...props} />
}

export function TableSkeleton({ rows = 8, cols = 6 }: { rows?: number; cols?: number }) {
  return (
    <div role="status" aria-label="Loading" className="space-y-2 p-4">
      {Array.from({ length: rows }).map((_, r) => (
        <div key={r} className="flex gap-3">
          {Array.from({ length: cols }).map((__, c) => (
            <Skeleton key={c} className={cn('h-5', c === 0 ? 'w-28' : 'flex-1')} />
          ))}
        </div>
      ))}
    </div>
  )
}
