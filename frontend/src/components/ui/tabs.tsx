import * as TabsPrimitive from '@radix-ui/react-tabs'
import { forwardRef, type ComponentPropsWithoutRef, type ComponentRef } from 'react'
import { cn } from '@/lib/utils'

export const Tabs = TabsPrimitive.Root

export const TabsList = forwardRef<ComponentRef<typeof TabsPrimitive.List>, ComponentPropsWithoutRef<typeof TabsPrimitive.List>>(({ className, ...props }, ref) => (
  <TabsPrimitive.List ref={ref} className={cn('flex items-end gap-1 border-b border-rule', className)} {...props} />
))
TabsList.displayName = 'TabsList'

export const TabsTrigger = forwardRef<ComponentRef<typeof TabsPrimitive.Trigger>, ComponentPropsWithoutRef<typeof TabsPrimitive.Trigger>>(({ className, ...props }, ref) => (
  <TabsPrimitive.Trigger
    ref={ref}
    className={cn(
      '-mb-px border-b-2 border-transparent px-3 py-2 text-sm font-medium text-muted transition-colors duration-150 hover:text-ink data-[state=active]:border-accent data-[state=active]:text-ink',
      className,
    )}
    {...props}
  />
))
TabsTrigger.displayName = 'TabsTrigger'

export const TabsContent = forwardRef<ComponentRef<typeof TabsPrimitive.Content>, ComponentPropsWithoutRef<typeof TabsPrimitive.Content>>(({ className, ...props }, ref) => (
  <TabsPrimitive.Content ref={ref} className={cn('pt-4 outline-none', className)} {...props} />
))
TabsContent.displayName = 'TabsContent'
