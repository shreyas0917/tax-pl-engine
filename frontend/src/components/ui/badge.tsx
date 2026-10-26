import { cva, type VariantProps } from 'class-variance-authority'
import type { HTMLAttributes } from 'react'
import { cn } from '@/lib/utils'

const badgeVariants = cva('inline-flex items-center gap-1 rounded-sm border px-1.5 py-0.5 text-[12px] font-medium leading-4 whitespace-nowrap', {
  variants: {
    tone: {
      neutral: 'border-rule bg-surface-2 text-ink-2',
      accent: 'border-transparent bg-accent-soft text-accent',
      gain: 'border-transparent bg-gain-soft text-gain',
      loss: 'border-transparent bg-loss-soft text-loss',
      warn: 'border-transparent bg-warn-soft text-warn',
      outline: 'border-rule-strong bg-transparent text-ink-2',
    },
  },
  defaultVariants: { tone: 'neutral' },
})

export interface BadgeProps extends HTMLAttributes<HTMLSpanElement>, VariantProps<typeof badgeVariants> {}

export function Badge({ className, tone, ...props }: BadgeProps) {
  return <span className={cn(badgeVariants({ tone }), className)} {...props} />
}

/** Job state badge with a consistent tone per state. */
export function StateBadge({ state }: { state: string }) {
  const tone =
    state === 'REPORT_READY' ? 'gain' : state === 'FAILED' ? 'loss' : state === 'NEEDS_REVIEW' ? 'warn' : state === 'UPLOADED' ? 'neutral' : 'accent'
  const label: Record<string, string> = {
    UPLOADED: 'Uploaded',
    PARSING: 'Parsing',
    NORMALIZED: 'Normalised',
    ADJUSTED: 'Adjusted',
    MATCHED: 'Matched',
    COMPUTED: 'Computed',
    REPORT_READY: 'Ready',
    NEEDS_REVIEW: 'Needs review',
    FAILED: 'Failed',
  }
  return <Badge tone={tone}>{label[state] ?? state}</Badge>
}
