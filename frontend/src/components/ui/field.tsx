import { type ReactNode, useId } from 'react'
import { Label } from './label'
import { cn } from '@/lib/utils'

interface FieldProps {
  label: string
  hint?: string
  error?: string
  className?: string
  children: (ids: { id: string; describedBy: string | undefined; invalid: boolean }) => ReactNode
}

/** Labelled form control with hint and error, wired up with aria attributes. */
export function Field({ label, hint, error, className, children }: FieldProps) {
  const id = useId()
  const hintId = `${id}-hint`
  const errorId = `${id}-error`
  const describedBy = [hint ? hintId : null, error ? errorId : null].filter(Boolean).join(' ') || undefined
  return (
    <div className={cn('space-y-1.5', className)}>
      <Label htmlFor={id}>{label}</Label>
      {children({ id, describedBy, invalid: !!error })}
      {hint && !error && (
        <p id={hintId} className="text-[13px] text-muted">
          {hint}
        </p>
      )}
      {error && (
        <p id={errorId} role="alert" className="text-[13px] text-loss">
          {error}
        </p>
      )}
    </div>
  )
}
