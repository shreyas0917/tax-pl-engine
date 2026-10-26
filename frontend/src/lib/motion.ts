import type { Transition, Variants } from 'motion/react'

/** Shared motion presets. Durations 150-300 ms, ease-out, nothing bounces. */
export const ease = [0.2, 0, 0, 1] as const

export const fast: Transition = { duration: 0.15, ease }
export const base: Transition = { duration: 0.22, ease }
export const slow: Transition = { duration: 0.3, ease }

export const fadeIn: Variants = {
  hidden: { opacity: 0 },
  visible: { opacity: 1, transition: base },
  exit: { opacity: 0, transition: fast },
}

export const rise: Variants = {
  hidden: { opacity: 0, y: 6 },
  visible: { opacity: 1, y: 0, transition: base },
  exit: { opacity: 0, y: 4, transition: fast },
}

export const sheetFromRight: Variants = {
  hidden: { x: 32, opacity: 0 },
  visible: { x: 0, opacity: 1, transition: slow },
  exit: { x: 32, opacity: 0, transition: base },
}

export const staggerChildren = (stagger = 0.04): Variants => ({
  hidden: {},
  visible: { transition: { staggerChildren: stagger } },
})

export const layoutTransition: Transition = { duration: 0.22, ease }
