import { Search } from 'lucide-react'
import { useDeferredValue, useMemo, useState } from 'react'
import { Link, useSearchParams } from 'react-router'
import { errorMessage } from '@/api/client'
import { useAccounts, useGains, type Category, type Gain } from '@/api/hooks'
import { PageHeader, Section } from '@/components/layout/page'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { TableSkeleton } from '@/components/ui/skeleton'
import { EmptyState, ErrorState } from '@/components/ui/states'
import { categoryLabel, formatInt } from '@/lib/format'
import { cn } from '@/lib/utils'
import { useYear } from '@/lib/year'
import { ExplainDrawer } from '@/features/explain/explain-drawer'
import { GainsTable } from './gains-table'

const CATEGORIES: Category[] = ['STCG_EQUITY', 'LTCG_EQUITY', 'INTRADAY', 'FNO', 'BUYBACK']
const ALL = '__all__'

export function GainsPage() {
  const { fy, rule } = useYear()
  const [params, setParams] = useSearchParams()
  const category = (params.get('category') as Category | null) ?? undefined
  const account = params.get('account') ?? ''
  const [search, setSearch] = useState(params.get('q') ?? '')
  const deferredSearch = useDeferredValue(search.trim().toUpperCase())
  const isinFilter = /^[A-Z]{2}[A-Z0-9]{9}[0-9]$/.test(deferredSearch) ? deferredSearch : ''
  const accounts = useAccounts()
  const gains = useGains({ fy, category, account, isin: isinFilter })
  const [selected, setSelected] = useState<Gain | null>(null)

  const rows = useMemo(() => {
    const all = gains.data?.pages.flatMap((p) => p.items) ?? []
    if (!deferredSearch || isinFilter) return all
    return all.filter((g) => g.symbol.toUpperCase().includes(deferredSearch) || g.isin.toUpperCase().includes(deferredSearch) || (g.contract_key ?? '').toUpperCase().includes(deferredSearch))
  }, [gains.data, deferredSearch, isinFilter])

  const setParam = (key: string, value: string | undefined) => {
    const next = new URLSearchParams(params)
    if (value) next.set(key, value)
    else next.delete(key)
    setParams(next, { replace: true })
  }

  const total = gains.data?.pages.reduce((n, p) => n + p.items.length, 0) ?? 0

  return (
    <>
      <PageHeader title="Gains" lede={rule ? `Every sale in ${rule.label}, classified. Open a row to see how its number was reached.` : undefined} />

      <div className="mb-4 flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
        <div className="flex flex-wrap gap-1.5" role="group" aria-label="Filter by category">
          <Chip active={!category} onClick={() => setParam('category', undefined)}>
            All
          </Chip>
          {CATEGORIES.map((c) => (
            <Chip key={c} active={category === c} onClick={() => setParam('category', c)}>
              {categoryLabel(c, true)}
            </Chip>
          ))}
        </div>
        <div className="flex flex-col gap-2 sm:flex-row">
          <div>
            <label htmlFor="gains-account" className="sr-only">
              Account
            </label>
            <Select value={account || ALL} onValueChange={(v) => setParam('account', v === ALL ? undefined : v)}>
              <SelectTrigger id="gains-account" className="w-full sm:w-48">
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
            <label htmlFor="gains-search" className="sr-only">
              Search by symbol or ISIN
            </label>
            <Search className="pointer-events-none absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted" aria-hidden />
            <Input id="gains-search" type="search" placeholder="Symbol or ISIN" className="pl-8 sm:w-56" value={search} onChange={(e) => setSearch(e.target.value)} />
          </div>
        </div>
      </div>

      <Section>
        {gains.isLoading && <TableSkeleton rows={10} cols={8} />}
        {gains.isError && <ErrorState message={errorMessage(gains.error)} onRetry={() => void gains.refetch()} />}
        {gains.data && rows.length === 0 && (
          <EmptyState
            title={total === 0 && !category && !account ? 'No gains in this year yet' : 'No gains match these filters'}
            body={total === 0 && !category && !account ? 'Upload a tradebook with sales in this year, or pick another year from the selector above.' : 'Clear a filter or search for a different symbol.'}
            action={
              total === 0 && !category && !account ? (
                <Button asChild variant="primary">
                  <Link to="/upload">Upload tradebook</Link>
                </Button>
              ) : (
                <Button
                  variant="secondary"
                  onClick={() => {
                    setSearch('')
                    setParams(new URLSearchParams(), { replace: true })
                  }}
                >
                  Clear filters
                </Button>
              )
            }
          />
        )}
        {gains.data && rows.length > 0 && (
          <>
            <GainsTable
              rows={rows}
              selectedId={selected?.id}
              onSelect={setSelected}
              onEndReached={() => {
                if (gains.hasNextPage && !gains.isFetchingNextPage) void gains.fetchNextPage()
              }}
            />
            <div className="flex items-center justify-between gap-3 border-t border-rule px-5 py-2.5 text-[13px] text-muted">
              <span>
                {formatInt(rows.length)} {rows.length === 1 ? 'line' : 'lines'}
                {deferredSearch && !isinFilter && ` matching "${search.trim()}"`}
                {gains.hasNextPage && ' loaded so far'}
              </span>
              {gains.hasNextPage && (
                <Button variant="secondary" size="sm" onClick={() => void gains.fetchNextPage()} disabled={gains.isFetchingNextPage}>
                  {gains.isFetchingNextPage ? 'Loading' : 'Load more'}
                </Button>
              )}
            </div>
          </>
        )}
      </Section>

      <ExplainDrawer gain={selected} onClose={() => setSelected(null)} />
    </>
  )
}

function Chip({ active, onClick, children }: { active: boolean; onClick: () => void; children: React.ReactNode }) {
  return (
    <button
      type="button"
      aria-pressed={active}
      onClick={onClick}
      className={cn(
        'rounded-full border px-3 py-1 text-[13px] font-medium transition-colors duration-150',
        active ? 'border-accent bg-accent-soft text-accent' : 'border-rule-strong bg-surface text-ink-2 hover:bg-surface-2',
      )}
    >
      {children}
    </button>
  )
}
