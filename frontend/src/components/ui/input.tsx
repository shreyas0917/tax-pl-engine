import { forwardRef, type InputHTMLAttributes } from 'react'
import { cn } from '@/lib/utils'

export const inputClass =
  'flex h-9 w-full rounded-md border border-rule-strong bg-surface px-3 text-[15px] text-ink placeholder:text-faint focus-visible:border-accent focus-visible:outline-2 focus-visible:outline-offset-0 focus-visible:outline-accent disabled:opacity-60 aria-[invalid=true]:border-loss'

export const Input = forwardRef<HTMLInputElement, InputHTMLAttributes<HTMLInputElement>>(({ className, ...props }, ref) => (
  <input ref={ref} className={cn(inputClass, className)} {...props} />
))
Input.displayName = 'Input'
