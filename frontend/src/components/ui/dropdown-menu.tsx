import * as DropdownMenuPrimitive from '@radix-ui/react-dropdown-menu'
import { Check } from 'lucide-react'
import { forwardRef, type ComponentPropsWithoutRef, type ComponentRef } from 'react'
import { cn } from '@/lib/utils'

export const DropdownMenu = DropdownMenuPrimitive.Root
export const DropdownMenuTrigger = DropdownMenuPrimitive.Trigger
export const DropdownMenuRadioGroup = DropdownMenuPrimitive.RadioGroup

export const DropdownMenuContent = forwardRef<ComponentRef<typeof DropdownMenuPrimitive.Content>, ComponentPropsWithoutRef<typeof DropdownMenuPrimitive.Content>>(
  ({ className, sideOffset = 4, ...props }, ref) => (
    <DropdownMenuPrimitive.Portal>
      <DropdownMenuPrimitive.Content
        ref={ref}
        sideOffset={sideOffset}
        className={cn('z-50 min-w-[10rem] rounded-md border border-rule bg-surface p-1 text-ink shadow-[0_8px_24px_-12px_rgba(0,0,0,0.25)]', className)}
        {...props}
      />
    </DropdownMenuPrimitive.Portal>
  ),
)
DropdownMenuContent.displayName = 'DropdownMenuContent'

export const DropdownMenuItem = forwardRef<ComponentRef<typeof DropdownMenuPrimitive.Item>, ComponentPropsWithoutRef<typeof DropdownMenuPrimitive.Item>>(({ className, ...props }, ref) => (
  <DropdownMenuPrimitive.Item
    ref={ref}
    className={cn('relative flex cursor-default select-none items-center rounded-sm px-2 py-1.5 text-sm outline-none data-[highlighted]:bg-surface-2 data-[disabled]:opacity-50', className)}
    {...props}
  />
))
DropdownMenuItem.displayName = 'DropdownMenuItem'

export const DropdownMenuRadioItem = forwardRef<ComponentRef<typeof DropdownMenuPrimitive.RadioItem>, ComponentPropsWithoutRef<typeof DropdownMenuPrimitive.RadioItem>>(
  ({ className, children, ...props }, ref) => (
    <DropdownMenuPrimitive.RadioItem
      ref={ref}
      className={cn('relative flex cursor-default select-none items-center rounded-sm py-1.5 pl-7 pr-2 text-sm outline-none data-[highlighted]:bg-surface-2', className)}
      {...props}
    >
      <span className="absolute left-2 flex size-4 items-center justify-center">
        <DropdownMenuPrimitive.ItemIndicator>
          <Check className="size-3.5 text-accent" aria-hidden />
        </DropdownMenuPrimitive.ItemIndicator>
      </span>
      {children}
    </DropdownMenuPrimitive.RadioItem>
  ),
)
DropdownMenuRadioItem.displayName = 'DropdownMenuRadioItem'

export const DropdownMenuLabel = ({ className, ...props }: ComponentPropsWithoutRef<typeof DropdownMenuPrimitive.Label>) => (
  <DropdownMenuPrimitive.Label className={cn('px-2 py-1.5 text-[12px] font-medium text-muted', className)} {...props} />
)

export const DropdownMenuSeparator = ({ className, ...props }: ComponentPropsWithoutRef<typeof DropdownMenuPrimitive.Separator>) => (
  <DropdownMenuPrimitive.Separator className={cn('my-1 h-px bg-rule', className)} {...props} />
)
