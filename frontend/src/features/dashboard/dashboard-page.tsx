import { AlertTriangle, ArrowRight } from 'lucide-react'
import { motion } from 'motion/react'
import { useEffect } from 'react'
import { Link } from 'react-router'
import { errorMessage } from '@/api/client'
import { useSummary, useUploads, type Summary, type Upload } from '@/api/hooks'
import { PageHeader, Section } from '@/components/layout/page'
import { Badge, StateBadge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { CountUp } from '@/components/ui/count-up'
import { Money } from '@/components/ui/money'
import { Skeleton } from '@/components/ui/skeleton'
import { EmptyState, ErrorState } from '@/components/ui/states'
import { brokerLabel, categoryLabel, formatDateTime, formatINR, formatInt, formatRate } from '@/lib/format'
import { rise, staggerChildren } from '@/lib/motion'
import { useYear } from '@/lib/year'
import { CategoryChart } from './category-chart'

export function DashboardPage() {
  const { fy, rule, auto, setFy } = useYear()
  const summary = useSummary(fy)
  const uploads = useUploads()

  // First visit: if the current year is empty but another has data, show that one.
  const years = summary.data?.years_with_data
  useEffect(() => {
    if (auto && years && years.length && !years.includes(fy)) {
      const latest = [...years].sort().at(-1)
      // Switching the year is a response to data arriving from the server.
      if (latest) setFy(latest, false)
    }
  }, [auto, fy, setFy, years])

  const data = summary.data
  const s = data?.summary
  const hasData = !!s && ((s.categories?.length ?? 0) > 0 || !!data?.computation)

  return (
    <>
      <PageHeader
        title="Dashboard"
        lede={rule ? `${rule.label}, under the ${rule.law}.` : undefined}
        actions={
          <Button asChild variant="primary">
            <Link to="/upload">Upload tradebook</Link>
          </Button>
        }
      />

      {summary.isLoading && <DashboardSkeleton />}
      {summary.isError && <ErrorState message={errorMessage(summary.error)} onRetry={() => void summary.refetch()} />}

      {data && !hasData && (
        <Section>
          <EmptyState
            title="Upload your first tradebook"
            body={`Nothing has been computed for ${rule?.label ?? fy} yet. Upload a tradebook from any supported broker and the engine will match, classify and explain every sale.`}
            action={
              <Button asChild variant="primary">
                <Link to="/upload">Upload tradebook</Link>
              </Button>
            }
          />
          {data.years_with_data.length > 0 && (
            <p className="border-t border-rule px-6 py-3 text-sm text-muted sm:px-8">
              Years with data: {data.years_with_data.map((y) => (
                <button key={y} type="button" className="mr-2 text-accent underline-offset-4 hover:underline" onClick={() => setFy(y)}>
                  {y}
                </button>
              ))}
            </p>
          )}
        </Section>
      )}

      {data && s && hasData && (
        <motion.div variants={staggerChildren(0.05)} initial="hidden" animate="visible" className="space-y-6">
          {data.open_review_items > 0 && (
            <motion.div variants={rise}>
              <Link
                to="/review"
                className="flex items-center justify-between gap-3 rounded-lg border border-warn/40 bg-warn-soft px-4 py-3 text-sm text-ink hover:brightness-[0.98]"
              >
                <span className="flex items-center gap-2">
                  <AlertTriangle className="size-4 text-warn" aria-hidden />
                  <span>
                    <strong className="font-semibold">{formatInt(data.open_review_items)}</strong> {data.open_review_items === 1 ? 'item needs' : 'items need'} your
                    input before these figures are complete.
                  </span>
                </span>
                <span className="flex items-center gap-1 font-medium text-accent">
                  Review <ArrowRight className="size-4" aria-hidden />
                </span>
              </Link>
            </motion.div>
          )}

          <motion.div variants={rise}>
            <Figures s={s} />
          </motion.div>

          <motion.div variants={rise} className="grid gap-6 lg:grid-cols-[1.2fr_1fr] lg:items-start">
            <AccountsTable s={s} />
            <div className="space-y-6">
              <EstimateCard s={s} />
              <LossesCard s={s} />
            </div>
          </motion.div>

          <motion.div variants={rise}>
            <RecentUploads uploads={uploads.data?.items} loading={uploads.isLoading} />
          </motion.div>

          {data.computation && (
            <p className="text-[13px] text-muted">
              Last computed {formatDateTime(data.computation.created_at)} from {formatInt(data.computation.trade_count)} trades. Checksum{' '}
              <span className="num">{data.computation.checksum.slice(0, 16)}</span>. Rules {s.rules_version}.
            </p>
          )}
        </motion.div>
      )}
    </>
  )
}

function Figures({ s }: { s: Summary }) {
  const count = (c: string) => s.categories?.find((x) => x.category === c)?.count ?? 0
  const period = s.periods[0]
  const cells = [
    { key: 'STCG_EQUITY', label: 'Short-term capital gains', value: s.stcg, sub: period ? `${formatInt(count('STCG_EQUITY'))} lines, taxed at ${formatRate(period.stcg_rate)}` : `${formatInt(count('STCG_EQUITY'))} lines` },
    { key: 'LTCG_EQUITY', label: 'Long-term capital gains', value: s.ltcg, sub: period ? `${formatInt(count('LTCG_EQUITY'))} lines, taxed at ${formatRate(period.ltcg_rate)} above ${formatINR(s.ltcg_exemption, { whole: true })}` : `${formatInt(count('LTCG_EQUITY'))} lines` },
    { key: 'INTRADAY', label: 'Intraday', value: s.intraday, sub: `${formatInt(count('INTRADAY'))} lines, speculative business income` },
    { key: 'FNO', label: 'F&O', value: s.fno, sub: `${formatInt(count('FNO'))} lines, non-speculative business income` },
  ]
  return (
    <Section>
      <div className="grid gap-px bg-rule sm:grid-cols-2 lg:grid-cols-4">
        {cells.map((c) => (
          <div key={c.key} className="bg-surface px-4 py-4 sm:px-5" data-testid={`figure-${c.key}`}>
            <h2 className="text-sm text-muted">{c.label}</h2>
            <p className="mt-1.5 text-[1.55rem] leading-tight font-semibold">
              <CountUp value={c.value} signed whole />
            </p>
            <p className="mt-1 text-[13px] text-muted">{c.sub}</p>
          </div>
        ))}
      </div>
      <div className="grid gap-4 border-t border-rule px-4 py-4 sm:px-5 lg:grid-cols-[auto_1fr] lg:gap-8" data-testid="figure-turnover">
        <div>
          <h2 className="text-sm text-muted">F&amp;O turnover</h2>
          <p className="mt-1.5 text-[1.55rem] leading-tight font-semibold">
            <CountUp value={s.fno_turnover} whole />
          </p>
        </div>
        <p className="max-w-[68ch] self-center text-[13px] text-muted">
          Audit thresholds {formatINR(s.audit_threshold, { whole: true })} and {formatINR(s.audit_threshold_low_cash, { whole: true })} (low cash). {s.audit_note}
        </p>
      </div>
      {s.categories && s.categories.length > 0 && (
        <div className="border-t border-rule px-4 py-4 sm:px-5">
          <CategoryChart categories={s.categories} />
        </div>
      )}
    </Section>
  )
}

function AccountsTable({ s }: { s: Summary }) {
  const accounts = s.accounts ?? []
  const cats = ['STCG_EQUITY', 'LTCG_EQUITY', 'INTRADAY', 'FNO'] as const
  return (
    <Section title="By account">
      {accounts.length === 0 ? (
        <p className="px-5 py-6 text-sm text-muted">No per-account figures yet.</p>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="text-left text-[13px] text-muted">
                <th scope="col" className="px-5 py-2 font-medium">
                  Account
                </th>
                {cats.map((c) => (
                  <th key={c} scope="col" className="num px-3 py-2 font-medium">
                    {categoryLabel(c, true)}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {accounts.map((a) => (
                <tr key={a.account_id} className="border-t border-rule">
                  <th scope="row" className="px-5 py-2.5 text-left font-medium">
                    {a.account_id}
                  </th>
                  {cats.map((c) => (
                    <td key={c} className="num px-3 py-2.5">
                      {a.by_category[c] === undefined ? <span className="text-faint">{'—'}</span> : <Money value={a.by_category[c]} signed />}
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      <p className="border-t border-rule px-5 py-2.5 text-[13px] text-muted">Lots are matched FIFO within each account, never across accounts.</p>
    </Section>
  )
}

function EstimateCard({ s }: { s: Summary }) {
  return (
    <Section title="Estimated tax" aside={<Badge tone="warn">Estimate</Badge>}>
      <div className="px-5 py-4">
        <p className="text-[1.55rem] leading-tight font-semibold">
          <CountUp value={s.estimated_tax} whole />
        </p>
        <dl className="mt-3 grid grid-cols-[1fr_auto] gap-x-4 gap-y-1 text-sm">
          {s.periods.map((p) => (
            <PeriodRows key={p.period} p={p} multi={s.periods.length > 1} />
          ))}
          <dt className="text-muted">LTCG exemption</dt>
          <dd className="num">{formatINR(s.ltcg_exemption, { whole: true })}</dd>
          <dt className="text-muted">LTCG taxable after exemption</dt>
          <dd className="num">{formatINR(s.ltcg_taxable_after_exemption)}</dd>
        </dl>
        {s.estimate_notes.length > 0 && (
          <ul className="mt-3 space-y-1 border-t border-rule pt-3 text-[13px] text-muted">
            {s.estimate_notes.map((n) => (
              <li key={n}>{n}</li>
            ))}
          </ul>
        )}
      </div>
    </Section>
  )
}

function PeriodRows({ p, multi }: { p: Summary['periods'][number]; multi: boolean }) {
  return (
    <>
      <dt className="text-muted">{multi ? `STCG ${p.period} at ${formatRate(p.stcg_rate)}` : `STCG at ${formatRate(p.stcg_rate)}`}</dt>
      <dd className="num">{formatINR(p.estimated_tax_stcg)}</dd>
      <dt className="text-muted">{multi ? `LTCG ${p.period} at ${formatRate(p.ltcg_rate)}` : `LTCG at ${formatRate(p.ltcg_rate)}`}</dt>
      <dd className="num">{formatINR(p.estimated_tax_ltcg)}</dd>
    </>
  )
}

const AGAINST_LABEL: Record<string, string> = {
  STCG: 'short-term capital gains',
  LTCG: 'long-term capital gains',
  SPECULATIVE: 'speculative income',
  BUSINESS: 'business income',
  ANY_EXCEPT_SALARY: 'any income except salary',
}
const against = (xs: string[]) => xs.map((x) => AGAINST_LABEL[x] ?? x.replace(/_/g, ' ').toLowerCase()).join(', ')

const LOSS_LABEL: Record<string, string> = {
  STCL: 'Short-term capital loss',
  LTCL: 'Long-term capital loss',
  SPECULATIVE: 'Intraday (speculative) loss',
  NON_SPECULATIVE_BUSINESS: 'F&O (business) loss',
}

function LossesCard({ s }: { s: Summary }) {
  const losses = s.losses ?? []
  return (
    <Section title="Losses">
      {losses.length === 0 ? (
        <p className="px-5 py-4 text-sm text-muted">No net losses this year.</p>
      ) : (
        <ul className="divide-y divide-rule">
          {losses.map((l) => (
            <li key={l.loss} className="px-5 py-3 text-sm">
              <div className="flex items-baseline justify-between gap-3">
                <span className="font-medium">{LOSS_LABEL[l.loss] ?? l.loss}</span>
                <Money value={`-${l.amount}`} signed />
              </div>
              <p className="mt-0.5 text-[13px] text-muted">
                Set off against {against(l.set_off_against)}. Carry forward {l.carry_forward_years} years against {against(l.carry_forward_against)}.
              </p>
            </li>
          ))}
        </ul>
      )}
      {losses.length > 0 && (
        <p className="flex items-start gap-2 border-t border-rule bg-warn-soft px-5 py-3 text-[13px] text-warn">
          <AlertTriangle className="mt-0.5 size-3.5 shrink-0" aria-hidden />
          Losses can be carried forward only if you file your return by the due date.
        </p>
      )}
    </Section>
  )
}

function RecentUploads({ uploads, loading }: { uploads: Upload[] | undefined; loading: boolean }) {
  return (
    <Section
      title="Recent uploads"
      aside={
        <Button asChild variant="link" size="sm">
          <Link to="/upload">Upload another</Link>
        </Button>
      }
    >
      {loading && (
        <div className="space-y-2 p-5">
          <Skeleton className="h-5 w-2/3" />
          <Skeleton className="h-5 w-1/2" />
        </div>
      )}
      {uploads && uploads.length === 0 && <p className="px-5 py-4 text-sm text-muted">No uploads yet.</p>}
      {uploads && uploads.length > 0 && (
        <ul className="divide-y divide-rule">
          {uploads.slice(0, 6).map((u) => (
            <li key={u.id} className="flex flex-wrap items-center justify-between gap-x-4 gap-y-1 px-5 py-2.5 text-sm">
              <span className="min-w-0 truncate font-medium">{u.filename}</span>
              <span className="flex items-center gap-3 text-[13px] text-muted">
                <span>{brokerLabel(u.broker)}</span>
                <span>{formatDateTime(u.created_at)}</span>
                <StateBadge state={u.status} />
              </span>
            </li>
          ))}
        </ul>
      )}
    </Section>
  )
}

function DashboardSkeleton() {
  return (
    <div className="space-y-6" role="status" aria-label="Loading dashboard">
      <div className="panel grid gap-px bg-rule sm:grid-cols-2 lg:grid-cols-5">
        {Array.from({ length: 5 }).map((_, i) => (
          <div key={i} className="space-y-2 bg-surface px-5 py-4">
            <Skeleton className="h-4 w-24" />
            <Skeleton className="h-7 w-32" />
            <Skeleton className="h-3 w-20" />
          </div>
        ))}
      </div>
      <div className="grid gap-6 lg:grid-cols-[1.2fr_1fr] lg:items-start">
        <div className="panel h-48" />
        <div className="panel h-48" />
      </div>
    </div>
  )
}
