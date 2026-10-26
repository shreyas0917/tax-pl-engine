import * as LabelPrimitive from '@radix-ui/react-label'
import { forwardRef, type ComponentPropsWithoutRef, type ComponentRef } from 'react'
import { cn } from '@/lib/utils'

export const Label = forwardRef<ComponentRef<typeof LabelPrimitive.Root>, ComponentPropsWithoutRef<typeof LabelPrimitive.Root>>(
  ({ className, ...props }, ref) => (
    <LabelPrimitive.Root ref={ref} className={cn('block text-sm font-medium text-ink-2', className)} {...props} />
  ),
)
Label.displayName = 'Label'
