import { createColumnHelper, flexRender, getCoreRowModel, getSortedRowModel, useReactTable, type SortingState } from '@tanstack/react-table'
import { useVirtualizer } from '@tanstack/react-virtual'
import { ArrowDown, ArrowUp, ArrowUpDown } from 'lucide-react'
import { useMemo, useRef, useState } from 'react'
import type { Gain } from '@/api/hooks'
import { Badge } from '@/components/ui/badge'
import { Money } from '@/components/ui/money'
import { categoryLabel, formatDate, formatDays, formatQty } from '@/lib/format'
import { cn } from '@/lib/utils'

const col = createColumnHelper<Gain>()

function numSort(a: string, b: string): number {
  const x = Number(a)
  const y = Number(b)
  return x === y ? 0 : x < y ? -1 : 1
}

const columns = [
  col.accessor('sale_date', { header: 'Sold', cell: (c) => <span className="whitespace-nowrap">{formatDate(c.getValue())}</span>, sortingFn: 'alphanumeric' }),
  col.accessor('symbol', {
    header: 'Instrument',
    cell: (c) => (
      <span className="flex flex-col">
        <span className="font-medium">{c.getValue()}</span>
        <span className="break-all text-[12px] text-muted">{c.row.original.contract_key ?? c.row.original.isin}</span>
      </span>
    ),
  }),
  col.accessor('account_name', { header: 'Account' }),
  col.accessor('category', {
    header: 'Category',
    cell: (c) => (
      <span className="flex items-center gap-1.5">
        <Badge tone="outline">{categoryLabel(c.getValue(), true)}</Badge>
        {c.row.original.grandfathered && <Badge tone="accent">Grandfathered</Badge>}
      </span>
    ),
  }),
  col.accessor('quantity', { header: 'Qty', cell: (c) => <span className="num">{formatQty(c.getValue())}</span>, sortingFn: (a, b) => numSort(a.original.quantity, b.original.quantity), meta: { num: true } }),
  col.accessor('sale_value', { header: 'Sale value', cell: (c) => <Money value={c.getValue()} />, sortingFn: (a, b) => numSort(a.original.sale_value, b.original.sale_value), meta: { num: true } }),
  col.accessor('cost_of_acquisition', { header: 'Cost', cell: (c) => <Money value={c.getValue()} />, sortingFn: (a, b) => numSort(a.original.cost_of_acquisition, b.original.cost_of_acquisition), meta: { num: true } }),
  col.accessor('charges_deductible', { header: 'Charges', cell: (c) => <Money value={c.getValue()} />, sortingFn: (a, b) => numSort(a.original.charges_deductible, b.original.charges_deductible), meta: { num: true } }),
  col.accessor('gain', { header: 'Gain', cell: (c) => <Money value={c.getValue()} signed className="font-medium" />, sortingFn: (a, b) => numSort(a.original.gain, b.original.gain), meta: { num: true } }),
  col.accessor('holding_days', { header: 'Held', cell: (c) => <span className="num">{c.row.original.category === 'INTRADAY' || c.row.original.category === 'FNO' ? '—' : formatDays(c.getValue())}</span>, meta: { num: true } }),
]

declare module '@tanstack/react-table' {
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  interface ColumnMeta<TData, TValue> {
    num?: boolean
  }
}

interface GainsTableProps {
  rows: Gain[]
  onSelect: (gain: Gain) => void
  selectedId?: string | null
  /** Called when the user scrolls near the end; load the next page. */
  onEndReached?: () => void
  virtualise?: boolean
}

export function GainsTable({ rows, onSelect, selectedId, onEndReached, virtualise = true }: GainsTableProps) {
  const [sorting, setSorting] = useState<SortingState>([])
  const table = useReactTable({ data: rows, columns, state: { sorting }, onSortingChange: setSorting, getCoreRowModel: getCoreRowModel(), getSortedRowModel: getSortedRowModel() })
  const tableRows = table.getRowModel().rows
  const parentRef = useRef<HTMLDivElement>(null)
  const virtualizer = useVirtualizer({
    count: tableRows.length,
    getScrollElement: () => parentRef.current,
    estimateSize: () => 48,
    overscan: 12,
    enabled: virtualise,
  })
  const items = virtualise ? virtualizer.getVirtualItems() : tableRows.map((_, index) => ({ index, start: 0, size: 48, key: index }))
  const totalSize = virtualise ? virtualizer.getTotalSize() : 0
  const padTop = virtualise && items.length ? (items[0]?.start ?? 0) : 0
  const padBottom = virtualise && items.length ? totalSize - (items[items.length - 1]?.start ?? 0) - (items[items.length - 1]?.size ?? 0) : 0
  const headerGroups = useMemo(() => table.getHeaderGroups(), [table])

  return (
    <div
      ref={parentRef}
      className="max-h-[70vh] overflow-auto"
      onScroll={(e) => {
        const el = e.currentTarget
        if (onEndReached && el.scrollHeight - el.scrollTop - el.clientHeight < 400) onEndReached()
      }}
    >
      <table className="w-full min-w-[960px] text-sm" data-testid="gains-table">
        <thead className="sticky top-0 z-10 bg-surface">
          {headerGroups.map((hg) => (
            <tr key={hg.id}>
              {hg.headers.map((h) => {
                const sorted = h.column.getIsSorted()
                const num = h.column.columnDef.meta?.num
                return (
                  <th key={h.id} scope="col" className={cn('border-b border-rule-strong px-3 py-2 text-left text-[13px] font-medium text-muted first:pl-5 last:pr-5', num && 'text-right')} aria-sort={sorted === 'asc' ? 'ascending' : sorted === 'desc' ? 'descending' : 'none'}>
                    <button type="button" className={cn('inline-flex items-center gap-1 rounded-sm hover:text-ink', num && 'flex-row-reverse')} onClick={h.column.getToggleSortingHandler()}>
                      {flexRender(h.column.columnDef.header, h.getContext())}
                      {sorted === 'asc' ? <ArrowUp className="size-3" aria-hidden /> : sorted === 'desc' ? <ArrowDown className="size-3" aria-hidden /> : <ArrowUpDown className="size-3 opacity-40" aria-hidden />}
                    </button>
                  </th>
                )
              })}
            </tr>
          ))}
        </thead>
        <tbody>
          {padTop > 0 && (
            <tr aria-hidden>
              <td style={{ height: padTop }} colSpan={columns.length} />
            </tr>
          )}
          {items.map((vi) => {
            const row = tableRows[vi.index]
            if (!row) return null
            const g = row.original
            const selected = g.id === selectedId
            return (
              <tr
                key={row.id}
                data-index={vi.index}
                ref={virtualise ? virtualizer.measureElement : undefined}
                tabIndex={0}
                role="button"
                aria-label={`Explain ${g.symbol} sold ${formatDate(g.sale_date)}`}
                aria-pressed={selected}
                onClick={() => onSelect(g)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter' || e.key === ' ') {
                    e.preventDefault()
                    onSelect(g)
                  }
                }}
                className={cn('cursor-pointer border-b border-rule transition-colors duration-150 hover:bg-surface-2 focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-accent', selected && 'bg-accent-soft')}
                data-testid="gain-row"
              >
                {row.getVisibleCells().map((cell) => (
                  <td key={cell.id} className={cn('px-3 py-2 align-top first:pl-5 last:pr-5', cell.column.columnDef.meta?.num && 'text-right')}>
                    {flexRender(cell.column.columnDef.cell, cell.getContext())}
                  </td>
                ))}
              </tr>
            )
          })}
          {padBottom > 0 && (
            <tr aria-hidden>
              <td style={{ height: padBottom }} colSpan={columns.length} />
            </tr>
          )}
        </tbody>
      </table>
    </div>
  )
}
