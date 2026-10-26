import { cn } from '@/lib/utils'

export function Spinner({ className, label = 'Loading' }: { className?: string; label?: string }) {
  return (
    <span role="status" aria-label={label} className={cn('inline-block size-4 animate-spin rounded-full border-2 border-rule-strong border-t-accent motion-reduce:animate-none', className)} />
  )
}
