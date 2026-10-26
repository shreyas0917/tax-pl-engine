import { AnimatePresence, motion } from 'motion/react'
import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { Link } from 'react-router'
import { z } from 'zod'
import { errorMessage, isApiError } from '@/api/client'
import { useResolveReviewItem, useReviewItems, type ReviewItem } from '@/api/hooks'
import { JobStepper } from '@/components/job-stepper'
import { PageHeader, Section } from '@/components/layout/page'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Field } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { EmptyState, ErrorState, InlineError } from '@/components/ui/states'
import { formatDate, formatQty } from '@/lib/format'
import { rise, staggerChildren } from '@/lib/motion'
import { zodResolver } from '@/lib/zod-resolver'

const KIND_LABEL: Record<string, string> = {
  MISSING_BUY_HISTORY: 'Missing purchase',
  UNKNOWN_ISIN: 'Unknown ISIN',
  MISSING_FMV_2018: 'Missing 31 Jan 2018 price',
  TRANSFER_DETAILS: 'Transfer details',
  PARSE_ISSUE: 'Could not read a row',
  OPEN_FNO_POSITION: 'Open F&O position',
  BUYBACK_DEEMED_DIVIDEND: 'Buyback (deemed dividend)',
}

export function ReviewPage() {
  const items = useReviewItems('open')
  const [recomputeJob, setRecomputeJob] = useState<string | null>(null)

  return (
    <>
      <PageHeader title="Review" lede="The engine stops rather than guesses. Each item below needs one fact from you; answering it recomputes the affected figures." />

      {recomputeJob && (
        <Section title="Recomputing" className="mb-6">
          <div className="p-4 sm:p-5">
            <JobStepper
              jobId={recomputeJob}
              onTerminal={() => {
                void items.refetch()
              }}
            />
          </div>
        </Section>
      )}

      {items.isLoading && (
        <div className="space-y-4" role="status" aria-label="Loading review items">
          {[0, 1].map((i) => (
            <div key={i} className="panel space-y-3 p-5">
              <Skeleton className="h-4 w-40" />
              <Skeleton className="h-4 w-3/4" />
              <Skeleton className="h-9 w-1/2" />
            </div>
          ))}
        </div>
      )}
      {items.isError && <ErrorState message={errorMessage(items.error)} onRetry={() => void items.refetch()} />}

      {items.data && items.data.items.length === 0 && (
        <Section>
          <EmptyState
            title="Nothing to review"
            body="Every sale has been matched and classified without needing your input."
            action={
              <Button asChild variant="primary">
                <Link to="/gains">See gains</Link>
              </Button>
            }
          />
        </Section>
      )}

      {items.data && items.data.items.length > 0 && (
        <motion.ul variants={staggerChildren(0.04)} initial="hidden" animate="visible" className="space-y-4" aria-label="Open review items">
          <AnimatePresence initial={false}>
            {items.data.items.map((item) => (
              <motion.li key={item.id} variants={rise} layout exit="exit">
                <ReviewCard item={item} onResolved={(jobId) => setRecomputeJob(jobId)} />
              </motion.li>
            ))}
          </AnimatePresence>
        </motion.ul>
      )}
    </>
  )
}

function payloadString(p: ReviewItem['payload'], key: string): string {
  const v = p?.[key]
  return v === undefined || v === null ? '' : String(v)
}

function ReviewCard({ item, onResolved }: { item: ReviewItem; onResolved: (jobId: string) => void }) {
  const resolve = useResolveReviewItem()
  const dismiss = () =>
    resolve.mutate({ id: item.id, body: { dismiss: true } }, { onSuccess: (r) => onResolved(r.job_id) })
  const quantity = payloadString(item.payload, 'quantity')
  const saleDate = payloadString(item.payload, 'sale_date')

  return (
    <article className="panel" data-testid="review-item" data-kind={item.kind}>
      <div className="flex flex-wrap items-start justify-between gap-3 border-b border-rule px-5 py-3">
        <div>
          <div className="flex flex-wrap items-center gap-2">
            <h2 className="text-[15px] font-semibold">{KIND_LABEL[item.kind] ?? item.kind}</h2>
            {item.symbol && <Badge tone="outline">{item.symbol}</Badge>}
            {item.account_name && <span className="text-[13px] text-muted">{item.account_name}</span>}
          </div>
          <p className="mt-1 max-w-prose text-sm text-ink-2">{item.message}</p>
          {(quantity || saleDate) && (
            <p className="mt-1 text-[13px] text-muted">
              {quantity && `${formatQty(quantity)} units`}
              {quantity && saleDate && ', '}
              {saleDate && `sold ${formatDate(saleDate)}`}
            </p>
          )}
          {item.kind === 'PARSE_ISSUE' && item.payload && (
            <p className="mt-1 text-[13px] text-muted">
              {payloadString(item.payload, 'file')} line {payloadString(item.payload, 'line')}: {payloadString(item.payload, 'reason')}
            </p>
          )}
        </div>
      </div>
      <div className="px-5 py-4">
        {item.kind === 'MISSING_BUY_HISTORY' && <BuyHistoryForm item={item} resolve={resolve} onResolved={onResolved} withQuantity />}
        {item.kind === 'TRANSFER_DETAILS' && <BuyHistoryForm item={item} resolve={resolve} onResolved={onResolved} />}
        {item.kind === 'MISSING_FMV_2018' && <FmvForm item={item} resolve={resolve} onResolved={onResolved} />}
        {item.kind === 'UNKNOWN_ISIN' && <IsinForm item={item} resolve={resolve} onResolved={onResolved} />}
        {!['MISSING_BUY_HISTORY', 'TRANSFER_DETAILS', 'MISSING_FMV_2018', 'UNKNOWN_ISIN'].includes(item.kind) && (
          <div className="flex flex-wrap items-center gap-3">
            <p className="text-sm text-muted">Nothing to enter here; dismiss once you have read it.</p>
            <Button variant="secondary" size="sm" onClick={dismiss} disabled={resolve.isPending}>
              Dismiss
            </Button>
          </div>
        )}
        {['MISSING_BUY_HISTORY', 'TRANSFER_DETAILS', 'MISSING_FMV_2018', 'UNKNOWN_ISIN'].includes(item.kind) && (
          <div className="mt-3 border-t border-rule pt-3">
            <Button variant="link" size="sm" onClick={dismiss} disabled={resolve.isPending}>
              Dismiss without answering
            </Button>
          </div>
        )}
        {resolve.isError && !(isApiError(resolve.error) && Object.keys(resolve.error.errors).length) && (
          <div className="mt-3">
            <InlineError message={errorMessage(resolve.error)} />
          </div>
        )}
      </div>
    </article>
  )
}

type Resolver = ReturnType<typeof useResolveReviewItem>

const decimal = z.string().trim().regex(/^[0-9]+(\.[0-9]+)?$/, 'Enter a number, for example 12345.00')
const dateStr = z.string().regex(/^\d{4}-\d{2}-\d{2}$/, 'Enter a date')

const buySchema = z.object({ acquisition_date: dateStr, cost_total: decimal, quantity: z.string().trim().optional() })
type BuyValues = z.infer<typeof buySchema>

function applyServerErrors<T extends Record<string, unknown>>(e: unknown, setError: (k: keyof T & string, m: string) => void, keys: (keyof T & string)[]) {
  if (!isApiError(e)) return
  for (const [k, v] of Object.entries(e.errors)) if ((keys as string[]).includes(k)) setError(k as keyof T & string, v)
}

function BuyHistoryForm({ item, resolve, onResolved, withQuantity }: { item: ReviewItem; resolve: Resolver; onResolved: (j: string) => void; withQuantity?: boolean }) {
  const form = useForm<BuyValues>({
    resolver: zodResolver(buySchema),
    defaultValues: { acquisition_date: '', cost_total: '', quantity: payloadString(item.payload, 'quantity') },
  })
  const submit = form.handleSubmit((v) => {
    const body = withQuantity
      ? { acquisition_date: v.acquisition_date, cost_total: v.cost_total, quantity: v.quantity || undefined }
      : { acquisition_date: v.acquisition_date, cost_total: v.cost_total }
    resolve.mutate(
      { id: item.id, body },
      {
        onSuccess: (r) => onResolved(r.job_id),
        onError: (e) => applyServerErrors<BuyValues>(e, (k, m) => form.setError(k, { type: 'server', message: m }), ['acquisition_date', 'cost_total', 'quantity']),
      },
    )
  })
  return (
    <form onSubmit={submit} noValidate className="grid gap-3 sm:grid-cols-[1fr_1fr_1fr_auto] sm:items-end">
      <Field label={withQuantity ? 'Bought on' : 'Originally bought on'} error={form.formState.errors.acquisition_date?.message}>
        {({ id, describedBy, invalid }) => <Input id={id} type="date" aria-describedby={describedBy} aria-invalid={invalid} data-testid="acquisition-date" {...form.register('acquisition_date')} />}
      </Field>
      <Field label="Total cost (₹)" hint="For the whole quantity, not per share." error={form.formState.errors.cost_total?.message}>
        {({ id, describedBy, invalid }) => (
          <Input id={id} inputMode="decimal" placeholder="0.00" aria-describedby={describedBy} aria-invalid={invalid} data-testid="cost-total" {...form.register('cost_total')} />
        )}
      </Field>
      {withQuantity ? (
        <Field label="Quantity" error={form.formState.errors.quantity?.message}>
          {({ id, describedBy, invalid }) => <Input id={id} inputMode="decimal" aria-describedby={describedBy} aria-invalid={invalid} {...form.register('quantity')} />}
        </Field>
      ) : (
        <div />
      )}
      <Button type="submit" variant="primary" disabled={resolve.isPending} data-testid="resolve-submit">
        {resolve.isPending ? 'Saving' : 'Save and recompute'}
      </Button>
    </form>
  )
}

const fmvSchema = z.object({ fmv: decimal })
type FmvValues = z.infer<typeof fmvSchema>

function FmvForm({ item, resolve, onResolved }: { item: ReviewItem; resolve: Resolver; onResolved: (j: string) => void }) {
  const form = useForm<FmvValues>({ resolver: zodResolver(fmvSchema), defaultValues: { fmv: '' } })
  const submit = form.handleSubmit((v) =>
    resolve.mutate(
      { id: item.id, body: { fmv: v.fmv } },
      { onSuccess: (r) => onResolved(r.job_id), onError: (e) => applyServerErrors<FmvValues>(e, (k, m) => form.setError(k, { type: 'server', message: m }), ['fmv']) },
    ),
  )
  return (
    <form onSubmit={submit} noValidate className="grid gap-3 sm:grid-cols-[1fr_auto] sm:items-end">
      <Field label="Highest traded price on 31 Jan 2018 (₹ per share)" hint="From the exchange's historical data for this ISIN." error={form.formState.errors.fmv?.message}>
        {({ id, describedBy, invalid }) => <Input id={id} inputMode="decimal" placeholder="0.00" aria-describedby={describedBy} aria-invalid={invalid} {...form.register('fmv')} />}
      </Field>
      <Button type="submit" variant="primary" disabled={resolve.isPending}>
        {resolve.isPending ? 'Saving' : 'Save and recompute'}
      </Button>
    </form>
  )
}

const isinSchema = z.object({ isin: z.string().trim().toUpperCase().regex(/^[A-Z]{2}[A-Z0-9]{9}[0-9]$/, 'An ISIN is 12 characters, for example INE009A01021') })
type IsinValues = z.infer<typeof isinSchema>

function IsinForm({ item, resolve, onResolved }: { item: ReviewItem; resolve: Resolver; onResolved: (j: string) => void }) {
  const form = useForm<IsinValues>({ resolver: zodResolver(isinSchema), defaultValues: { isin: '' } })
  const submit = form.handleSubmit((v) =>
    resolve.mutate(
      { id: item.id, body: { isin: v.isin } },
      { onSuccess: (r) => onResolved(r.job_id), onError: (e) => applyServerErrors<IsinValues>(e, (k, m) => form.setError(k, { type: 'server', message: m }), ['isin']) },
    ),
  )
  return (
    <form onSubmit={submit} noValidate className="grid gap-3 sm:grid-cols-[1fr_auto] sm:items-end">
      <Field label={`ISIN for ${item.symbol || 'this symbol'}`} hint="The file that raised this will be parsed again." error={form.formState.errors.isin?.message}>
        {({ id, describedBy, invalid }) => <Input id={id} placeholder="INE009A01021" aria-describedby={describedBy} aria-invalid={invalid} {...form.register('isin')} />}
      </Field>
      <Button type="submit" variant="primary" disabled={resolve.isPending}>
        {resolve.isPending ? 'Saving' : 'Save and re-parse'}
      </Button>
    </form>
  )
}
