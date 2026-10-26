import { motion } from 'motion/react'
import type { ReactNode } from 'react'
import { errorMessage } from '@/api/client'
import { useExplain, type ExplainTree, type Gain } from '@/api/hooks'
import { Badge } from '@/components/ui/badge'
import { Money } from '@/components/ui/money'
import { Sheet } from '@/components/ui/sheet'
import { Skeleton } from '@/components/ui/skeleton'
import { ErrorState } from '@/components/ui/states'
import { categoryLabel, formatDate, formatDays, formatINR, formatQty, originLabel } from '@/lib/format'
import { rise, staggerChildren } from '@/lib/motion'

type TradeView = NonNullable<ExplainTree['sale']>
type LotView = ExplainTree['lots'][number]

export function ExplainDrawer({ gain, onClose }: { gain: Gain | null; onClose: () => void }) {
  const explain = useExplain(gain?.id ?? null)
  return (
    <Sheet
      open={!!gain}
      onOpenChange={(o) => !o && onClose()}
      ariaTitle={gain ? `Why this number: ${gain.symbol} sold ${formatDate(gain.sale_date)}` : 'Why this number'}
      title={
        gain && (
          <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
            <span className="text-base font-semibold">{gain.symbol}</span>
            <span className="text-sm text-muted">sold {formatDate(gain.sale_date)}</span>
            <Badge tone="outline">{categoryLabel(gain.category, true)}</Badge>
          </div>
        )
      }
      description={gain ? `Why this number, step by step. Account ${gain.account_name}.` : undefined}
    >
      {explain.isLoading && (
        <div className="space-y-4" role="status" aria-label="Loading explanation">
          <Skeleton className="h-5 w-1/2" />
          <Skeleton className="h-20" />
          <Skeleton className="h-20" />
          <Skeleton className="h-24" />
        </div>
      )}
      {explain.isError && <ErrorState message={errorMessage(explain.error)} onRetry={() => void explain.refetch()} />}
      {explain.data && <ExplainBody tree={explain.data} />}
    </Sheet>
  )
}

/** The trail: a numbered sequence of real steps, ending in the ledger total. */
export function ExplainBody({ tree }: { tree: ExplainTree }) {
  const derivative = tree.category === 'INTRADAY' || tree.category === 'FNO'
  const steps: { title: string; body: ReactNode }[] = []

  if (tree.sale) steps.push({ title: derivative ? 'Closing trade' : 'Sale', body: <TradeBlock t={tree.sale} /> })
  if (tree.buy) steps.push({ title: 'Opening trade', body: <TradeBlock t={tree.buy} /> })

  tree.lots.forEach((lot, i) => {
    steps.push({ title: tree.lots.length > 1 ? `Matched lot ${i + 1} of ${tree.lots.length}` : 'Matched lot', body: <LotBlock lot={lot} /> })
    if (lot.adjustments.length) steps.push({ title: 'Corporate actions', body: <AdjustmentsBlock lot={lot} /> })
    if (lot.grandfathering) steps.push({ title: 'Grandfathering (31 Jan 2018)', body: <GrandfatheringBlock lot={lot} /> })
    steps.push({ title: 'Holding period', body: <HoldingBlock lot={lot} category={tree.category} /> })
  })

  steps.push({
    title: 'Category and rate period',
    body: (
      <p className="text-sm">
        {categoryLabel(tree.category)}
        {tree.rate_period && <span className="text-muted">, rate period {tree.rate_period}</span>}
        {tree.buyback_regime && <span className="text-muted">, buyback regime {tree.buyback_regime.replace(/_/g, ' ')}</span>}
      </p>
    ),
  })

  return (
    <motion.div variants={staggerChildren(0.05)} initial="hidden" animate="visible" data-testid="explain-body">
      <ol className="relative border-l border-rule-strong pl-6">
        {steps.map((s, i) => (
          <motion.li key={i} variants={rise} className="relative pb-6 last:pb-4">
            <span className="num absolute -left-6 flex size-[23px] -translate-x-1/2 items-center justify-center rounded-full border border-rule-strong bg-surface text-[11px] font-medium text-ink-2" aria-hidden>
              {i + 1}
            </span>
            <h3 className="text-sm font-semibold">{s.title}</h3>
            <div className="mt-1.5">{s.body}</div>
          </motion.li>
        ))}
      </ol>

      <motion.div variants={rise} className="mt-2 rounded-lg border border-rule bg-surface-2 px-4 py-3">
        <dl className="grid grid-cols-[1fr_auto] gap-x-4 gap-y-1 text-sm">
          <dt className="text-muted">Sale value</dt>
          <dd>
            <Money value={tree.sale_value} />
          </dd>
          <dt className="text-muted">Cost of acquisition</dt>
          <dd>
            <Money value={`-${tree.cost_of_acquisition}`} />
          </dd>
          <dt className="text-muted">Deductible charges</dt>
          <dd>
            <Money value={`-${tree.charges_deductible}`} />
          </dd>
          <dt className="ledger-total mt-1 pt-1.5 font-medium">{derivative ? 'Profit or loss' : 'Gain'}</dt>
          <dd className="ledger-total mt-1 pt-1.5">
            <Money value={tree.gain} signed className="text-base font-semibold" />
          </dd>
        </dl>
      </motion.div>

      {tree.steps && tree.steps.length > 0 && (
        <motion.section variants={rise} className="mt-6" aria-labelledby="explain-steps">
          <h3 id="explain-steps" className="text-sm font-semibold">
            Engine notes
          </h3>
          <ol className="mt-2 list-decimal space-y-1 pl-5 text-sm text-ink-2">
            {tree.steps.map((s, i) => (
              <li key={i}>{s}</li>
            ))}
          </ol>
        </motion.section>
      )}

      <motion.p variants={rise} className="mt-6 border-t border-rule pt-3 text-[12px] text-muted">
        Rules version {tree.rules_version}. {tree.disclaimer}
      </motion.p>
    </motion.div>
  )
}

function Row({ label, children }: { label: string; children: ReactNode }) {
  return (
    <>
      <dt className="text-muted">{label}</dt>
      <dd className="text-right">{children}</dd>
    </>
  )
}

function TradeBlock({ t }: { t: TradeView }) {
  return (
    <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-0.5 text-sm">
      <Row label="Date">
        {formatDate(t.date)}
        {t.time && <span className="text-muted"> {t.time}</span>}
        {t.exchange && <span className="text-muted"> on {t.exchange}</span>}
      </Row>
      <Row label="Quantity">
        <span className="num">{formatQty(t.quantity)}</span>
      </Row>
      <Row label="Price">
        <Money value={t.price} />
      </Row>
      <Row label="Value">
        <Money value={t.value} />
      </Row>
      {t.source && (
        <Row label="Source">
          <span className="break-all">
            {t.source}
            {t.source_row ? `, row ${t.source_row}` : ''}
          </span>
        </Row>
      )}
      {t.charges && (
        <Row label="Charges">
          <span>
            <Badge tone={t.charges.source === 'estimated' ? 'warn' : 'neutral'}>{t.charges.source === 'estimated' ? 'Estimated' : 'Imported'}</Badge>{' '}
            <span className="text-muted">
              brokerage {formatINR(t.charges.brokerage)}, STT {formatINR(t.charges.stt)}
            </span>
          </span>
        </Row>
      )}
      {t.kind !== 'REGULAR' && (
        <Row label="Kind">
          <span>{t.kind.replace(/_/g, ' ').toLowerCase()}</span>
        </Row>
      )}
    </dl>
  )
}

function LotBlock({ lot }: { lot: LotView }) {
  return (
    <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-0.5 text-sm">
      <Row label="Account">{lot.account}</Row>
      <Row label="Acquired">{formatDate(lot.acquired)}</Row>
      <Row label="Origin">{originLabel(lot.origin)}</Row>
      <Row label="Quantity matched">
        <span className="num">{formatQty(lot.quantity)}</span>
      </Row>
      <Row label="Actual cost">
        <Money value={lot.actual_cost} />
        <span className="text-muted"> ({formatINR(lot.cost_per_unit)} per unit)</span>
      </Row>
      {lot.open_trade?.source && (
        <Row label="Source">
          <span className="break-all">
            {lot.open_trade.source}
            {lot.open_trade.source_row ? `, row ${lot.open_trade.source_row}` : ''}
          </span>
        </Row>
      )}
    </dl>
  )
}

function AdjustmentsBlock({ lot }: { lot: LotView }) {
  return (
    <ul className="space-y-2 text-sm">
      {lot.adjustments.map((a, i) => (
        <li key={i} className="rounded-md border border-rule px-3 py-2">
          <div className="flex flex-wrap items-center gap-2">
            <Badge tone="accent">{a.kind}</Badge>
            <span>ex-date {formatDate(a.ex_date)}</span>
            {a.ratio && <span className="text-muted">ratio {a.ratio}</span>}
          </div>
          <p className="mt-1 text-[13px] text-muted">
            Quantity {formatQty(a.qty_from)} to {formatQty(a.qty_to)}; cost {formatINR(a.cost_from)} to {formatINR(a.cost_to)}.{a.note ? ` ${a.note}` : ''}
          </p>
        </li>
      ))}
    </ul>
  )
}

function GrandfatheringBlock({ lot }: { lot: LotView }) {
  const g = lot.grandfathering!
  const chosen = (v: string) => v === g.chosen_cost
  return (
    <div className="text-sm">
      <dl className="grid grid-cols-[1fr_auto] gap-x-4 gap-y-0.5">
        <dt className={chosen(g.actual_cost) ? 'font-medium' : 'text-muted'}>Actual cost</dt>
        <dd className={chosen(g.actual_cost) ? 'font-medium' : ''}>
          <Money value={g.actual_cost} />
        </dd>
        <dt className={chosen(g.fmv_total) ? 'font-medium' : 'text-muted'}>
          FMV on 31 Jan 2018 <span className="text-muted">({formatINR(g.fmv_31_jan_2018_per_unit)} per unit)</span>
        </dt>
        <dd className={chosen(g.fmv_total) ? 'font-medium' : ''}>
          <Money value={g.fmv_total} />
        </dd>
        <dt className={chosen(g.sale_value) ? 'font-medium' : 'text-muted'}>Sale value</dt>
        <dd className={chosen(g.sale_value) ? 'font-medium' : ''}>
          <Money value={g.sale_value} />
        </dd>
        <dt className="ledger-total mt-1 pt-1 font-medium">Cost used</dt>
        <dd className="ledger-total mt-1 pt-1 font-medium">
          <Money value={g.chosen_cost} />
        </dd>
      </dl>
      <p className="mt-2 text-[13px] text-muted">
        {g.applied ? 'The grandfathered value replaced the actual cost. ' : 'The actual cost was kept. '}
        {g.explanation}
      </p>
    </div>
  )
}

function HoldingBlock({ lot, category }: { lot: LotView; category: ExplainTree['category'] }) {
  const long = category === 'LTCG_EQUITY'
  return (
    <p className="text-sm">
      Held {formatDays(lot.holding_days)}, <span className="font-medium">{long ? 'long-term' : 'short-term'}</span>
      <span className="text-muted"> (twelve months or more counts as long-term).</span>
    </p>
  )
}
