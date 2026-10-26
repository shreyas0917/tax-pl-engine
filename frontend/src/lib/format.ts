/**
 * Display-only formatting for decimal strings coming from the API.
 * Values are never converted to floats for arithmetic; the only numeric
 * conversion here is for Intl formatting of a value we already hold as a string.
 */

const inr = new Intl.NumberFormat('en-IN', {
  style: 'currency',
  currency: 'INR',
  minimumFractionDigits: 2,
  maximumFractionDigits: 2,
})
const inrWhole = new Intl.NumberFormat('en-IN', {
  style: 'currency',
  currency: 'INR',
  minimumFractionDigits: 0,
  maximumFractionDigits: 0,
})
const plain = new Intl.NumberFormat('en-IN', { maximumFractionDigits: 4 })
const intFmt = new Intl.NumberFormat('en-IN', { maximumFractionDigits: 0 })

export type Sign = 'positive' | 'negative' | 'zero'

/** Sign of a decimal string, decided on the text itself (no float conversion). */
export function signOf(value: string | null | undefined): Sign {
  if (value == null) return 'zero'
  const v = value.trim()
  if (v === '') return 'zero'
  const negative = v.startsWith('-')
  const digits = v.replace(/^[-+]/, '').replace(/[^0-9]/g, '')
  if (/^0*$/.test(digits)) return 'zero'
  return negative ? 'negative' : 'positive'
}

export function isZero(value: string | null | undefined): boolean {
  return signOf(value) === 'zero'
}

function absString(value: string): string {
  return value.trim().replace(/^[-+]/, '')
}

/** INR amount with lakh/crore grouping. Negative values get a U+2212 minus. */
export function formatINR(value: string | null | undefined, opts: { whole?: boolean } = {}): string {
  if (value == null || value.trim() === '') return '—'
  const abs = absString(value)
  const n = Number(abs)
  if (!Number.isFinite(n)) return value
  const out = opts.whole ? inrWhole.format(n) : inr.format(n)
  return signOf(value) === 'negative' ? `−${out}` : out
}

/**
 * Signed INR for gains and losses. Always carries an explicit + or − sign
 * (U+2212 minus) so meaning never depends on colour alone.
 */
export function formatSignedINR(value: string | null | undefined, opts: { whole?: boolean } = {}): string {
  if (value == null || value.trim() === '') return '—'
  const sign = signOf(value)
  const abs = absString(value)
  const n = Number(abs)
  if (!Number.isFinite(n)) return value
  const out = opts.whole ? inrWhole.format(n) : inr.format(n)
  if (sign === 'negative') return `−${out}`
  if (sign === 'positive') return `+${out}`
  return out
}

/** Quantity or plain decimal, trimmed of trailing zeros. */
export function formatQty(value: string | null | undefined): string {
  if (value == null || value.trim() === '') return '—'
  const n = Number(value)
  if (!Number.isFinite(n)) return value
  return plain.format(n)
}

export function formatInt(value: number | null | undefined): string {
  if (value == null) return '—'
  return intFmt.format(value)
}

/** Percentage from a fraction string, e.g. "0.125" -> "12.5%". */
export function formatRate(fraction: string | null | undefined): string {
  if (fraction == null || fraction.trim() === '') return '—'
  const n = Number(fraction)
  if (!Number.isFinite(n)) return fraction
  return `${plain.format(n * 100)}%`
}

const dateFmt = new Intl.DateTimeFormat('en-IN', { day: '2-digit', month: 'short', year: 'numeric', timeZone: 'UTC' })
const dateTimeFmt = new Intl.DateTimeFormat('en-IN', {
  day: '2-digit',
  month: 'short',
  year: 'numeric',
  hour: '2-digit',
  minute: '2-digit',
})

/** YYYY-MM-DD -> "12 Aug 2025". Returns the input unchanged when it does not parse. */
export function formatDate(value: string | null | undefined): string {
  if (!value) return '—'
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(value)
  if (!m) return value
  const d = new Date(Date.UTC(Number(m[1]), Number(m[2]) - 1, Number(m[3])))
  return dateFmt.format(d)
}

export function formatDateTime(value: string | null | undefined): string {
  if (!value) return '—'
  const d = new Date(value)
  if (Number.isNaN(d.getTime())) return value
  return dateTimeFmt.format(d)
}

export function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  return `${(n / (1024 * 1024)).toFixed(1)} MB`
}

export function formatDays(days: number): string {
  if (days === 1) return '1 day'
  return `${intFmt.format(days)} days`
}

export const CATEGORY_LABEL: Record<string, string> = {
  STCG_EQUITY: 'Short-term capital gains',
  LTCG_EQUITY: 'Long-term capital gains',
  INTRADAY: 'Intraday',
  FNO: 'F&O',
  BUYBACK: 'Buyback',
  OUT_OF_SCOPE: 'Out of scope',
}

export const CATEGORY_SHORT: Record<string, string> = {
  STCG_EQUITY: 'STCG',
  LTCG_EQUITY: 'LTCG',
  INTRADAY: 'Intraday',
  FNO: 'F&O',
  BUYBACK: 'Buyback',
  OUT_OF_SCOPE: 'Out of scope',
}

export function categoryLabel(c: string | undefined, short = false): string {
  if (!c) return '—'
  return (short ? CATEGORY_SHORT[c] : CATEGORY_LABEL[c]) ?? c
}

export const BROKER_LABEL: Record<string, string> = {
  zerodha: 'Zerodha',
  groww: 'Groww',
  upstox: 'Upstox',
  angelone: 'Angel One',
  other: 'Other',
}

export function brokerLabel(b: string | undefined): string {
  if (!b) return '—'
  return BROKER_LABEL[b] ?? b
}

export function shortHash(h: string, n = 12): string {
  return h.length > n ? `${h.slice(0, n)}…` : h
}

export const ORIGIN_LABEL: Record<string, string> = {
  TRADE: 'Bought on exchange',
  BONUS: 'Bonus allotment',
  DEMERGER: 'Received in demerger',
  MERGER: 'Received in merger',
  TRANSFER: 'Transferred in',
  OPENING: 'Opening lot (entered by you)',
}

export function originLabel(o: string | undefined): string {
  if (!o) return '—'
  return ORIGIN_LABEL[o] ?? o
}
