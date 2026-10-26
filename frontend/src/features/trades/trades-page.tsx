import { Search } from 'lucide-react'
import { useDeferredValue, useMemo, useState } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router'
import { errorMessage } from '@/api/client'
import { useAccounts, useHoldings, useTrades } from '@/api/hooks'
import { PageHeader, Section } from '@/components/layout/page'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Money } from '@/components/ui/money'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { TableSkeleton } from '@/components/ui/skeleton'
import { EmptyState, ErrorState } from '@/components/ui/states'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { brokerLabel, formatDate, formatINR, formatInt, formatQty, originLabel } from '@/lib/format'
import { useYear } from '@/lib/year'

const ALL = '__all__'

function TradesTabs({ value }: { value: 'trades' | 'holdings' }) {
  const navigate = useNavigate()
  return (
    <Tabs value={value} onValueChange={(v) => navigate(v === 'holdings' ? '/holdings' : '/trades')} className="mb-4">
      <TabsList aria-label="Trades or holdings">
        <TabsTrigger value="trades">Trades</TabsTrigger>
        <TabsTrigger value="holdings">Holdings</TabsTrigger>
      </TabsList>
    </Tabs>
  )
}

export function TradesPage() {
  const { fy, rule } = useYear()
  const [params, setParams] = useSearchParams()
  const account = params.get('account') ?? ''
  const [search, setSearch] = useState('')
  const q = useDeferredValue(search.trim().toUpperCase())
  const isin = /^[A-Z]{2}[A-Z0-9]{9}[0-9]$/.test(q) ? q : ''
  const accounts = useAccounts()
  const trades = useTrades({ fy, account, isin })

  const rows = useMemo(() => {
    const all = trades.data?.pages.flatMap((p) => p.items) ?? []
    if (!q || isin) return all
    return all.filter((t) => t.symbol.toUpperCase().includes(q) || t.isin.toUpperCase().includes(q))
  }, [trades.data, q, isin])

  return (
    <>
      <PageHeader title="Trades" lede={rule ? `Normalised trades from every file, ${rule.label}.` : undefined} />
      <TradesTabs value="trades" />

      <div className="mb-4 flex flex-col gap-2 sm:flex-row">
        <div>
          <label htmlFor="trades-account" className="sr-only">
            Account
          </label>
          <Select
            value={account || ALL}
            onValueChange={(v) => {
              const next = new URLSearchParams(params)
              if (v === ALL) next.delete('account')
              else next.set('account', v)
              setParams(next, { replace: true })
            }}
          >
            <SelectTrigger id="trades-account" className="w-full sm:w-48">
              <SelectValue placeholder="All accounts" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={ALL}>All accounts</SelectItem>
              {accounts.data?.items.map((a) => (
                <SelectItem key={a.id} value={a.id}>
                  {a.nickname}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <div className="relative">
          <label htmlFor="trades-search" className="sr-only">
            Search by symbol or ISIN
          </label>
          <Search className="pointer-events-none absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted" aria-hidden />
          <Input id="trades-search" type="search" placeholder="Symbol or ISIN" className="pl-8 sm:w-56" value={search} onChange={(e) => setSearch(e.target.value)} />
        </div>
      </div>

      <Section>
        {trades.isLoading && <TableSkeleton rows={10} cols={8} />}
        {trades.isError && <ErrorState message={errorMessage(trades.error)} onRetry={() => void trades.refetch()} />}
        {trades.data && rows.length === 0 && (
          <EmptyState
            title="No trades here"
            body="Nothing matches this year, account and search."
            action={
              <Button asChild variant="primary">
                <Link to="/upload">Upload tradebook</Link>
              </Button>
            }
          />
        )}
        {trades.data && rows.length > 0 && (
          <>
            <div className="max-h-[70vh] overflow-auto">
              <table className="w-full min-w-[900px] text-sm">
                <thead className="sticky top-0 z-10 bg-surface">
                  <tr className="text-[13px] text-muted">
                    <Th>Date</Th>
                    <Th>Instrument</Th>
                    <Th>Account</Th>
                    <Th>Segment</Th>
                    <Th>Side</Th>
                    <Th num>Qty</Th>
                    <Th num>Price</Th>
                    <Th>Exchange</Th>
                  </tr>
                </thead>
                <tbody>
                  {rows.map((t) => (
                    <tr key={t.id} className="border-b border-rule">
                      <td className="px-3 py-2 pl-5">
                        {formatDate(t.trade_date)}
                        {t.trade_time && <span className="text-muted"> {t.trade_time}</span>}
                      </td>
                      <td className="px-3 py-2">
                        <span className="font-medium">{t.symbol}</span>
                        <span className="block text-[12px] text-muted">
                          {t.isin || (t.expiry ? `expiry ${formatDate(t.expiry)}${t.strike ? `, strike ${formatQty(t.strike)} ${t.option_type ?? ''}` : ''}` : '')}
                        </span>
                      </td>
                      <td className="px-3 py-2">
                        {t.account_name} <span className="text-muted">({brokerLabel(t.broker)})</span>
                      </td>
                      <td className="px-3 py-2">
                        <Badge tone="outline">{t.segment}</Badge>
                        {t.kind !== 'REGULAR' && <span className="ml-1 text-[12px] text-muted">{t.kind.replace(/_/g, ' ').toLowerCase()}</span>}
                      </td>
                      <td className="px-3 py-2">
                        <span className={t.side === 'BUY' ? 'text-ink' : 'text-ink-2'}>{t.side === 'BUY' ? 'Buy' : 'Sell'}</span>
                      </td>
                      <td className="num px-3 py-2">{formatQty(t.quantity)}</td>
                      <td className="num px-3 py-2">{formatINR(t.price)}</td>
                      <td className="px-3 py-2 pr-5">{t.exchange || '—'}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <div className="flex items-center justify-between gap-3 border-t border-rule px-5 py-2.5 text-[13px] text-muted">
              <span>
                {formatInt(rows.length)} {rows.length === 1 ? 'trade' : 'trades'}
                {trades.hasNextPage && ' loaded so far'}
              </span>
              {trades.hasNextPage && (
                <Button variant="secondary" size="sm" onClick={() => void trades.fetchNextPage()} disabled={trades.isFetchingNextPage}>
                  {trades.isFetchingNextPage ? 'Loading' : 'Load more'}
                </Button>
              )}
            </div>
          </>
        )}
      </Section>
    </>
  )
}

export function HoldingsPage() {
  const holdings = useHoldings()
  const items = holdings.data?.items ?? []
  const byAccount = useMemo(() => {
    const map = new Map<string, typeof items>()
    for (const h of items) {
      const list = map.get(h.account_name) ?? []
      list.push(h)
      map.set(h.account_name, list)
    }
    return [...map.entries()]
  }, [items])

  return (
    <>
      <PageHeader title="Holdings" lede="Open lots after the latest computation, per account. Compare them with your broker's holdings to spot missing history." />
      <TradesTabs value="holdings" />
      {holdings.isLoading && (
        <Section>
          <TableSkeleton rows={6} cols={6} />
        </Section>
      )}
      {holdings.isError && <ErrorState message={errorMessage(holdings.error)} onRetry={() => void holdings.refetch()} />}
      {holdings.data && items.length === 0 && (
        <Section>
          <EmptyState title="No open lots" body="Every purchased share has been sold, or nothing has been uploaded yet." />
        </Section>
      )}
      <div className="space-y-6">
        {byAccount.map(([account, lots]) => (
          <Section key={account} title={account} aside={<span className="text-[13px] text-muted">{formatInt(lots.length)} open lots</span>}>
            <div className="overflow-x-auto">
              <table className="w-full min-w-[720px] text-sm">
                <thead>
                  <tr className="text-[13px] text-muted">
                    <Th>Instrument</Th>
                    <Th>Acquired</Th>
                    <Th>Origin</Th>
                    <Th num>Open qty</Th>
                    <Th num>Open cost</Th>
                    <Th num>FMV 31 Jan 2018</Th>
                  </tr>
                </thead>
                <tbody>
                  {lots.map((h) => (
                    <tr key={h.id} className="border-t border-rule">
                      <td className="px-3 py-2 pl-5">
                        <span className="font-medium">{h.symbol}</span>
                        <span className="block text-[12px] text-muted">{h.isin}</span>
                      </td>
                      <td className="px-3 py-2">{formatDate(h.acquisition_date)}</td>
                      <td className="px-3 py-2">{originLabel(h.origin)}</td>
                      <td className="num px-3 py-2">{formatQty(h.quantity_open)}</td>
                      <td className="num px-3 py-2">
                        <Money value={h.cost_open} />
                      </td>
                      <td className="num px-3 py-2 pr-5">{h.grandfathered_fmv ? formatINR(h.grandfathered_fmv) : <span className="text-faint">{'—'}</span>}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </Section>
        ))}
      </div>
    </>
  )
}

function Th({ children, num }: { children: React.ReactNode; num?: boolean }) {
  return (
    <th scope="col" className={`border-b border-rule-strong px-3 py-2 font-medium first:pl-5 last:pr-5 ${num ? 'text-right' : 'text-left'}`}>
      {children}
    </th>
  )
}
