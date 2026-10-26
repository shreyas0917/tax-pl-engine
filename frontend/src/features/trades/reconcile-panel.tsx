import { FileSpreadsheet, UploadCloud } from 'lucide-react'
import { useCallback, useState } from 'react'
import { useDropzone } from 'react-dropzone'
import { Link } from 'react-router'
import { API_V1, ApiError, createAuthFetch, errorMessage, problemFromResponse } from '@/api/client'
import { useAccounts } from '@/api/hooks'
import { Section } from '@/components/layout/page'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Field } from '@/components/ui/field'
import { Money } from '@/components/ui/money'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { InlineError } from '@/components/ui/states'
import { brokerLabel, formatBytes, formatInt, formatQty } from '@/lib/format'
import { cn } from '@/lib/utils'

// TODO: POST /holdings/reconcile is not in backend/api/openapi.yaml yet. Replace these local types with
// components['schemas'][...] from src/api/schema.d.ts and `api.POST(...)` once the spec is updated and `npm run gen:api` is re-run.
type DifferenceKind = 'MISSING_HISTORY' | 'EXTRA_IN_ENGINE' | 'QUANTITY_MISMATCH' | 'COST_MISMATCH'

interface Difference {
  isin: string
  symbol: string
  broker_quantity: string
  engine_quantity: string
  delta: string
  broker_avg_price: string
  engine_avg_cost: string
  kind: DifferenceKind
  suggestion: string
}

interface ReconcileIssue {
  line: number
  reason: string
  row?: string
}

interface ReconcileResponse {
  account_id: string
  matched: number
  differences: Difference[]
  issues: ReconcileIssue[]
}

const KIND_LABEL: Record<DifferenceKind, string> = {
  MISSING_HISTORY: 'Missing history',
  EXTRA_IN_ENGINE: 'Not held at broker',
  QUANTITY_MISMATCH: 'Quantity differs',
  COST_MISMATCH: 'Cost differs',
}

const KIND_TONE: Record<DifferenceKind, 'warn' | 'loss' | 'accent'> = {
  MISSING_HISTORY: 'warn',
  EXTRA_IN_ENGINE: 'loss',
  QUANTITY_MISMATCH: 'loss',
  COST_MISMATCH: 'accent',
}

async function reconcile(file: File, accountId: string): Promise<ReconcileResponse> {
  const form = new FormData()
  form.append('file', file, file.name)
  form.append('account_id', accountId)
  const res = await createAuthFetch()(`${API_V1}/holdings/reconcile`, { method: 'POST', body: form })
  if (!res.ok) throw new ApiError(res.status, await problemFromResponse(res))
  return (await res.json()) as ReconcileResponse
}

/** Compare the broker's holdings statement with the engine's open lots for one account. */
export function ReconcilePanel() {
  const accounts = useAccounts()
  const [accountId, setAccountId] = useState('')
  const [file, setFile] = useState<File | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [result, setResult] = useState<ReconcileResponse | null>(null)
  const effectiveAccount = accountId || accounts.data?.items[0]?.id || ''

  const onDrop = useCallback((accepted: File[]) => {
    const f = accepted[0]
    if (f) {
      setFile(f)
      setResult(null)
      setError(null)
    }
  }, [])
  const { getRootProps, getInputProps, isDragActive, open } = useDropzone({
    onDrop,
    multiple: false,
    noClick: true,
    accept: { 'text/csv': ['.csv'], 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet': ['.xlsx'] },
  })

  const run = async () => {
    if (!file || !effectiveAccount) return
    setBusy(true)
    setError(null)
    try {
      setResult(await reconcile(file, effectiveAccount))
    } catch (e) {
      setError(e instanceof ApiError && Object.keys(e.errors).length ? Object.values(e.errors).join(' ') : errorMessage(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Section title="Reconcile with broker statement">
      <div className="grid gap-4 p-4 sm:p-5 lg:grid-cols-[260px_minmax(0,1fr)]">
        <Field label="Account" hint="The broker's holdings file for this demat account.">
          {({ id, describedBy }) => (
            <Select value={effectiveAccount} onValueChange={setAccountId} disabled={!accounts.data?.items.length}>
              <SelectTrigger id={id} aria-describedby={describedBy} data-testid="reconcile-account">
                <SelectValue placeholder={accounts.isLoading ? 'Loading accounts' : 'No accounts yet'} />
              </SelectTrigger>
              <SelectContent>
                {accounts.data?.items.map((a) => (
                  <SelectItem key={a.id} value={a.id}>
                    {a.nickname} ({brokerLabel(a.broker)})
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          )}
        </Field>
        <div>
          <div
            {...getRootProps({
              className: cn(
                'flex flex-col items-center justify-center gap-2 rounded-lg border border-dashed border-rule-strong px-4 py-6 text-center transition-colors duration-150',
                isDragActive && 'border-accent bg-accent-soft',
              ),
            })}
          >
            <input {...getInputProps({ 'aria-label': 'Choose the broker holdings file' })} data-testid="reconcile-file-input" />
            {file ? (
              <p className="flex items-center gap-2 text-sm">
                <FileSpreadsheet className="size-4 text-muted" aria-hidden />
                <span className="font-medium">{file.name}</span>
                <span className="text-muted">{formatBytes(file.size)}</span>
              </p>
            ) : (
              <>
                <UploadCloud className="size-5 text-muted" aria-hidden />
                <p className="text-sm">
                  {isDragActive ? 'Drop the holdings file' : 'Drag the broker’s holdings CSV here'}
                </p>
              </>
            )}
            <Button variant="secondary" size="sm" onClick={open}>
              {file ? 'Choose a different file' : 'Choose file'}
            </Button>
          </div>
          <div className="mt-3 flex flex-wrap items-center gap-3">
            <Button variant="primary" onClick={() => void run()} disabled={!file || !effectiveAccount || busy} data-testid="reconcile-submit">
              {busy ? 'Comparing' : 'Compare holdings'}
            </Button>
            <span className="text-[13px] text-muted">The file is read once and not stored.</span>
          </div>
        </div>
      </div>

      {error && (
        <div className="px-4 pb-4 sm:px-5">
          <InlineError message={error} />
        </div>
      )}

      {result && <ReconcileResult result={result} />}
    </Section>
  )
}

function ReconcileResult({ result }: { result: ReconcileResponse }) {
  const n = result.differences.length
  return (
    <div className="border-t border-rule" data-testid="reconcile-result" aria-live="polite">
      <p className="px-4 py-3 text-sm sm:px-5">
        <span className="font-medium">{formatInt(result.matched)}</span> {result.matched === 1 ? 'holding matches' : 'holdings match'} the broker statement
        {n > 0 && (
          <>
            , <span className="font-medium">{formatInt(n)}</span> {n === 1 ? 'differs' : 'differ'}
          </>
        )}
        .
      </p>
      {n === 0 ? (
        <p className="border-t border-rule px-4 py-4 text-sm text-muted sm:px-5">No differences: your uploads cover everything the broker holds.</p>
      ) : (
        <div className="overflow-x-auto border-t border-rule">
          <table className="w-full min-w-[900px] text-sm">
            <thead>
              <tr className="text-[13px] text-muted">
                <th scope="col" className="px-3 py-2 pl-5 text-left font-medium">
                  Instrument
                </th>
                <th scope="col" className="px-3 py-2 text-left font-medium">
                  Difference
                </th>
                <th scope="col" className="num px-3 py-2 font-medium">
                  Broker qty
                </th>
                <th scope="col" className="num px-3 py-2 font-medium">
                  Engine qty
                </th>
                <th scope="col" className="num px-3 py-2 font-medium">
                  Delta
                </th>
                <th scope="col" className="num px-3 py-2 font-medium">
                  Broker avg price
                </th>
                <th scope="col" className="num px-3 py-2 font-medium">
                  Engine avg cost
                </th>
                <th scope="col" className="px-3 py-2 pr-5 text-left font-medium">
                  Suggestion
                </th>
              </tr>
            </thead>
            <tbody>
              {result.differences.map((d, i) => (
                <tr key={`${d.isin}-${d.kind}-${i}`} className="border-t border-rule align-top">
                  <td className="px-3 py-2 pl-5">
                    <span className="font-medium">{d.symbol}</span>
                    <span className="block text-[12px] text-muted">{d.isin}</span>
                  </td>
                  <td className="px-3 py-2">
                    <Badge tone={KIND_TONE[d.kind] ?? 'neutral'}>{KIND_LABEL[d.kind] ?? d.kind}</Badge>
                  </td>
                  <td className="num px-3 py-2">{formatQty(d.broker_quantity)}</td>
                  <td className="num px-3 py-2">{formatQty(d.engine_quantity)}</td>
                  <td className="num px-3 py-2">{formatQty(d.delta)}</td>
                  <td className="num px-3 py-2">
                    <Money value={d.broker_avg_price} />
                  </td>
                  <td className="num px-3 py-2">
                    <Money value={d.engine_avg_cost} />
                  </td>
                  <td className="max-w-[32ch] px-3 py-2 pr-5 text-ink-2">{d.suggestion}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {result.issues.length > 0 && (
        <details className="border-t border-rule px-4 py-3 text-[13px] sm:px-5">
          <summary className="cursor-pointer font-medium">
            {formatInt(result.issues.length)} {result.issues.length === 1 ? 'row was' : 'rows were'} skipped while reading the file
          </summary>
          <ul className="mt-2 max-h-48 space-y-1 overflow-y-auto">
            {result.issues.map((it, i) => (
              <li key={i} className="flex gap-2">
                <span className="num shrink-0 text-muted">line {it.line}</span>
                <span>
                  {it.reason}
                  {it.row && <span className="text-muted"> ({it.row})</span>}
                </span>
              </li>
            ))}
          </ul>
        </details>
      )}
      {n > 0 && (
        <p className="border-t border-rule px-4 py-2.5 text-[13px] text-muted sm:px-5">
          Missing history usually means an older tradebook has not been uploaded yet.{' '}
          <Link to="/upload" className="text-accent underline-offset-4 hover:underline">
            Upload it
          </Link>{' '}
          or answer the open items in{' '}
          <Link to="/review" className="text-accent underline-offset-4 hover:underline">
            Review
          </Link>
          .
        </p>
      )}
    </div>
  )
}
