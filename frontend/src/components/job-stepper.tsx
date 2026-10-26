import { Check, CircleAlert, CircleHelp } from 'lucide-react'
import { AnimatePresence, motion } from 'motion/react'
import { useEffect, useRef } from 'react'
import { Link } from 'react-router'
import { useInvalidateDerived } from '@/api/hooks'
import { JOB_STAGES, STAGE_LABEL, useJobProgress, type JobEvent, type JobProgress, type JobState } from '@/api/sse'
import { cn } from '@/lib/utils'
import { base, fast } from '@/lib/motion'

interface JobStepperProps {
  jobId: string
  /** Called once when the job reaches a terminal state. */
  onTerminal?: (p: JobProgress) => void
  compact?: boolean
  /** Describes the job for the live region, e.g. the file name. */
  label?: string
}

const STAGE_INDEX = new Map<JobState, number>(JOB_STAGES.map((s, i) => [s, i]))

function stageIndex(state: JobState | null): number {
  if (!state) return -1
  if (state === 'NEEDS_REVIEW' || state === 'FAILED') return JOB_STAGES.length - 1
  return STAGE_INDEX.get(state) ?? -1
}

/**
 * Live pipeline progress for one job, fed by SSE. Each stage is a step;
 * the current step animates in, and the live region announces each change.
 */
export function JobStepper({ jobId, onTerminal, compact, label }: JobStepperProps) {
  const progress = useJobProgress(jobId)
  const invalidate = useInvalidateDerived()
  const fired = useRef(false)

  useEffect(() => {
    if (progress.terminal && !fired.current) {
      fired.current = true
      invalidate()
      onTerminal?.(progress)
    }
  }, [progress, invalidate, onTerminal])

  const current = stageIndex(progress.state)
  const failed = progress.state === 'FAILED'
  const review = progress.state === 'NEEDS_REVIEW'
  const lastEvent = progress.events.at(-1)
  const issues = collectIssues(progress.events)

  return (
    <div className={cn('text-sm', compact ? 'space-y-2' : 'space-y-3')} data-testid="job-stepper" data-state={progress.state ?? ''}>
      <ol className="flex flex-wrap items-center gap-x-1 gap-y-2" aria-label="Pipeline stages">
        {JOB_STAGES.map((stage, i) => {
          const done = i < current || (i === current && progress.terminal && !failed && !review)
          const active = i === current && !progress.terminal
          const isLast = i === JOB_STAGES.length - 1
          const terminalHere = isLast && (failed || review)
          return (
            <li key={stage} className="flex items-center gap-1">
              <motion.span
                layout
                transition={base}
                className={cn(
                  'inline-flex items-center gap-1.5 rounded-sm px-1.5 py-0.5 text-[12px] font-medium',
                  done && 'bg-gain-soft text-gain',
                  active && 'bg-accent-soft text-accent',
                  terminalHere && failed && 'bg-loss-soft text-loss',
                  terminalHere && review && 'bg-warn-soft text-warn',
                  !done && !active && !terminalHere && 'text-faint',
                )}
                aria-current={active ? 'step' : undefined}
              >
                {done && <Check className="size-3" aria-hidden />}
                {active && <span className="size-2 rounded-full bg-accent" aria-hidden />}
                {terminalHere && failed && <CircleAlert className="size-3" aria-hidden />}
                {terminalHere && review && <CircleHelp className="size-3" aria-hidden />}
                {terminalHere ? STAGE_LABEL[progress.state as JobState] : STAGE_LABEL[stage]}
              </motion.span>
              {!isLast && <span className="h-px w-3 bg-rule-strong" aria-hidden />}
            </li>
          )
        })}
      </ol>

      <div aria-live="polite" aria-atomic="true" className="min-h-[1.25rem] text-[13px] text-muted">
        <AnimatePresence mode="wait" initial={false}>
          <motion.p key={lastEvent?.id ?? 'none'} initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} transition={fast}>
            {label && <span className="sr-only">{label}: </span>}
            {progress.terminal && progress.state === 'REPORT_READY' && 'Done. Gains computed and ready to explore.'}
            {progress.terminal && review && (
              <>
                Done, but some items need your input.{' '}
                <Link to="/review" className="text-accent underline-offset-4 hover:underline">
                  Open review
                </Link>
              </>
            )}
            {progress.terminal && failed && <span className="text-loss">{progress.jobError ?? lastEvent?.message ?? 'The job failed.'}</span>}
            {!progress.terminal && (lastEvent?.message ?? (progress.connection === 'error' ? progress.error : 'Waiting for the pipeline to start'))}
          </motion.p>
        </AnimatePresence>
      </div>

      {issues.length > 0 && (
        <details className="rounded-md border border-rule bg-surface-2 px-3 py-2 text-[13px]">
          <summary className="cursor-pointer font-medium">
            {issues.length} {issues.length === 1 ? 'row was' : 'rows were'} skipped while parsing
          </summary>
          <ul className="mt-2 max-h-40 space-y-1 overflow-y-auto">
            {issues.map((it, i) => (
              <li key={i} className="flex gap-2">
                <span className="num shrink-0 text-muted">line {it.line}</span>
                <span>{it.reason}</span>
              </li>
            ))}
          </ul>
        </details>
      )}
    </div>
  )
}

interface ParseIssue {
  line: number | string
  reason: string
}

/** Pull parse issues ({line, reason}) out of event details, whatever key the stage used. */
export function collectIssues(events: JobEvent[]): ParseIssue[] {
  const out: ParseIssue[] = []
  for (const ev of events) {
    const d = ev.detail
    if (!d) continue
    const raw = (d['issues'] ?? d['parse_issues']) as unknown
    if (!Array.isArray(raw)) continue
    for (const item of raw) {
      if (item && typeof item === 'object') {
        const o = item as Record<string, unknown>
        out.push({ line: (o['line'] as number | string | undefined) ?? '?', reason: String(o['reason'] ?? o['message'] ?? 'skipped') })
      } else if (typeof item === 'string') {
        out.push({ line: '?', reason: item })
      }
    }
  }
  return out
}
