import { animate, useReducedMotion } from 'motion/react'
import { useEffect, useRef, useState } from 'react'
import { formatINR, formatSignedINR, signOf } from '@/lib/format'
import { cn } from '@/lib/utils'

interface CountUpProps {
  value: string | null | undefined
  signed?: boolean
  whole?: boolean
  className?: string
}

/**
 * Short ease-out count-up for headline figures (250 ms, no overshoot).
 * The final frame always renders the exact API string; intermediate frames are display only.
 * Disabled when the user prefers reduced motion.
 */
export function CountUp({ value, signed = false, whole = false, className }: CountUpProps) {
  const reduced = useReducedMotion()
  const [display, setDisplay] = useState<string | null | undefined>(value)
  const last = useRef<number>(0)
  const final = signed ? formatSignedINR(value, { whole }) : formatINR(value, { whole })
  const sign = signOf(value)

  useEffect(() => {
    const target = Number(value ?? 0)
    if (reduced || !Number.isFinite(target) || value == null) {
      last.current = Number.isFinite(target) ? target : 0
      return
    }
    const from = last.current
    const controls = animate(from, target, {
      duration: 0.25,
      ease: [0.2, 0, 0, 1],
      onUpdate: (v) => setDisplay(v.toFixed(2)),
      onComplete: () => setDisplay(value),
    })
    last.current = target
    return () => controls.stop()
  }, [value, reduced])

  const text = reduced || display === value ? final : signed ? formatSignedINR(display, { whole }) : formatINR(display, { whole })
  return (
    <span className={cn('num', signed && sign === 'positive' && 'text-gain', signed && sign === 'negative' && 'text-loss', className)} data-sign={signed ? sign : undefined}>
      {text}
    </span>
  )
}
