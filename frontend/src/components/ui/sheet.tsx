import * as DialogPrimitive from '@radix-ui/react-dialog'
import { X } from 'lucide-react'
import { AnimatePresence, motion } from 'motion/react'
import { type ReactNode } from 'react'
import { cn } from '@/lib/utils'
import { fadeIn, sheetFromRight } from '@/lib/motion'

interface SheetProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  title: ReactNode
  description?: ReactNode
  children: ReactNode
  className?: string
  /** Visually hidden title text for screen readers when `title` is decorative. */
  ariaTitle?: string
}

/**
 * Right-hand sheet on Radix Dialog: focus trapped, Esc closes, scroll locked.
 * Motion animates the panel; AnimatePresence keeps it mounted during exit.
 */
export function Sheet({ open, onOpenChange, title, description, children, className, ariaTitle }: SheetProps) {
  return (
    <DialogPrimitive.Root open={open} onOpenChange={onOpenChange}>
      <AnimatePresence>
        {open && (
          <DialogPrimitive.Portal forceMount>
            <DialogPrimitive.Overlay asChild forceMount>
              <motion.div variants={fadeIn} initial="hidden" animate="visible" exit="exit" className="fixed inset-0 z-40 bg-[rgba(21,32,28,0.45)] dark:bg-[rgba(0,0,0,0.6)]" />
            </DialogPrimitive.Overlay>
            <DialogPrimitive.Content asChild forceMount aria-describedby={undefined}>
              <motion.div
                variants={sheetFromRight}
                initial="hidden"
                animate="visible"
                exit="exit"
                className={cn(
                  'fixed inset-y-0 right-0 z-50 flex w-full max-w-[640px] flex-col border-l border-rule bg-surface text-ink outline-none sm:w-[92vw]',
                  className,
                )}
              >
                <div className="flex items-start justify-between gap-4 border-b border-rule px-5 py-4 sm:px-7">
                  <div className="min-w-0">
                    <DialogPrimitive.Title className={cn('text-base font-semibold', ariaTitle && 'sr-only')}>{ariaTitle ?? title}</DialogPrimitive.Title>
                    {ariaTitle && <div aria-hidden>{title}</div>}
                    {description && <DialogPrimitive.Description className="mt-0.5 text-sm text-muted">{description}</DialogPrimitive.Description>}
                  </div>
                  <DialogPrimitive.Close
                    className="inline-flex size-8 shrink-0 items-center justify-center rounded-md text-muted hover:bg-surface-2 hover:text-ink"
                    aria-label="Close"
                  >
                    <X className="size-4" aria-hidden />
                  </DialogPrimitive.Close>
                </div>
                <div className="min-h-0 flex-1 overflow-y-auto px-5 py-5 sm:px-7">{children}</div>
              </motion.div>
            </DialogPrimitive.Content>
          </DialogPrimitive.Portal>
        )}
      </AnimatePresence>
    </DialogPrimitive.Root>
  )
}

interface DialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  title: ReactNode
  description?: ReactNode
  children: ReactNode
}

/** Centered modal dialog for confirmations and small forms. */
export function Dialog({ open, onOpenChange, title, description, children }: DialogProps) {
  return (
    <DialogPrimitive.Root open={open} onOpenChange={onOpenChange}>
      <AnimatePresence>
        {open && (
          <DialogPrimitive.Portal forceMount>
            <DialogPrimitive.Overlay asChild forceMount>
              <motion.div variants={fadeIn} initial="hidden" animate="visible" exit="exit" className="fixed inset-0 z-40 bg-[rgba(21,32,28,0.45)] dark:bg-[rgba(0,0,0,0.6)]" />
            </DialogPrimitive.Overlay>
            <DialogPrimitive.Content asChild forceMount aria-describedby={description ? undefined : undefined}>
              <motion.div
                initial={{ opacity: 0, y: 8 }}
                animate={{ opacity: 1, y: 0 }}
                exit={{ opacity: 0, y: 6 }}
                transition={{ duration: 0.2, ease: [0.2, 0, 0, 1] }}
                className="fixed left-1/2 top-1/2 z-50 w-[calc(100vw-32px)] max-w-md -translate-x-1/2 -translate-y-1/2 rounded-lg border border-rule bg-surface p-6 text-ink outline-none"
              >
                <DialogPrimitive.Title className="text-base font-semibold">{title}</DialogPrimitive.Title>
                {description && <DialogPrimitive.Description className="mt-1 text-sm text-muted">{description}</DialogPrimitive.Description>}
                <div className="mt-4">{children}</div>
              </motion.div>
            </DialogPrimitive.Content>
          </DialogPrimitive.Portal>
        )}
      </AnimatePresence>
    </DialogPrimitive.Root>
  )
}
