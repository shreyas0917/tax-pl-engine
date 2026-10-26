import { Link } from 'react-router'

/** Three ruled lines and a double rule: the ledger, as a mark. */
export function Wordmark({ compact = false, to = '/dashboard' }: { compact?: boolean; to?: string }) {
  return (
    <Link to={to} className="inline-flex items-center gap-2 rounded-sm text-ink" aria-label="Tax P&L Engine home">
      <svg width="22" height="22" viewBox="0 0 32 32" aria-hidden className="shrink-0">
        <rect width="32" height="32" rx="6" className="fill-accent" />
        <path d="M8 10h16M8 16h16M8 22h10" className="stroke-accent-ink" strokeWidth="2.4" strokeLinecap="round" />
        <path d="M8 26h16" className="stroke-accent-ink" strokeWidth="1.2" />
      </svg>
      {!compact && <span className="text-[15px] font-semibold tracking-tight">Tax P&amp;L Engine</span>}
    </Link>
  )
}
