import { formatINR, formatSignedINR, signOf } from '@/lib/format'
import { cn } from '@/lib/utils'

interface MoneyProps {
  value: string | null | undefined
  /** Show a +/- sign and colour by sign (gains and losses). */
  signed?: boolean
  whole?: boolean
  className?: string
}

/** Tabular, right-aligned INR. Signed values are green/red with an explicit sign. */
export function Money({ value, signed = false, whole = false, className }: MoneyProps) {
  const sign = signOf(value)
  const text = signed ? formatSignedINR(value, { whole }) : formatINR(value, { whole })
  return (
    <span className={cn('num', signed && sign === 'positive' && 'text-gain', signed && sign === 'negative' && 'text-loss', className)} data-sign={signed ? sign : undefined}>
      {text}
    </span>
  )
}
