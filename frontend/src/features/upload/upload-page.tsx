import { FileSpreadsheet, UploadCloud, X } from 'lucide-react'
import { AnimatePresence, motion } from 'motion/react'
import { useCallback, useId, useMemo, useState } from 'react'
import { useDropzone } from 'react-dropzone'
import { useForm } from 'react-hook-form'
import { Link } from 'react-router'
import { z } from 'zod'
import { errorMessage, isApiError } from '@/api/client'
import { useAccounts, useBrokers, useCreateAccount, useUpload, type Upload } from '@/api/hooks'
import { JobStepper } from '@/components/job-stepper'
import { PageHeader, Section } from '@/components/layout/page'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Field } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { InlineError } from '@/components/ui/states'
import { brokerLabel, formatBytes } from '@/lib/format'
import { rise } from '@/lib/motion'
import { zodResolver } from '@/lib/zod-resolver'
import { cn } from '@/lib/utils'

interface QueuedFile {
  key: string
  file: File
  status: 'queued' | 'uploading' | 'uploaded' | 'error'
  upload?: Upload
  error?: string
}

const AUTO = '__auto__'

export function UploadPage() {
  const accounts = useAccounts()
  const brokers = useBrokers()
  const upload = useUpload()
  const [accountId, setAccountId] = useState<string>('')
  const [broker, setBroker] = useState<string>(AUTO)
  const [queue, setQueue] = useState<QueuedFile[]>([])
  const [showNewAccount, setShowNewAccount] = useState(false)

  const effectiveAccount = accountId || accounts.data?.items[0]?.id || ''
  const hasXlsx = queue.some((q) => q.status === 'queued' && /\.xlsx$/i.test(q.file.name))
  const needsBroker = hasXlsx && broker === AUTO

  const onDrop = useCallback((accepted: File[]) => {
    setQueue((q) => [
      ...q,
      ...accepted
        .filter((f) => !q.some((x) => x.file.name === f.name && x.file.size === f.size && x.status !== 'error'))
        .map((f) => ({ key: `${f.name}-${f.size}-${f.lastModified}-${Math.random().toString(36).slice(2, 8)}`, file: f, status: 'queued' as const })),
    ])
  }, [])

  const { getRootProps, getInputProps, isDragActive, open } = useDropzone({
    onDrop,
    multiple: true,
    noClick: true,
    accept: { 'text/csv': ['.csv'], 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet': ['.xlsx'] },
  })

  const startUploads = async () => {
    if (!effectiveAccount) return
    const pending = queue.filter((q) => q.status === 'queued')
    for (const item of pending) {
      setQueue((q) => q.map((x) => (x.key === item.key ? { ...x, status: 'uploading' } : x)))
      try {
        const res = await upload.mutateAsync({ file: item.file, accountId: effectiveAccount, broker: broker === AUTO ? undefined : broker })
        setQueue((q) => q.map((x) => (x.key === item.key ? { ...x, status: 'uploaded', upload: res } : x)))
      } catch (e) {
        const message = isApiError(e) && Object.keys(e.errors).length ? Object.values(e.errors).join(' ') : errorMessage(e)
        setQueue((q) => q.map((x) => (x.key === item.key ? { ...x, status: 'error', error: message } : x)))
      }
    }
  }

  const queuedCount = queue.filter((q) => q.status === 'queued').length
  const busy = queue.some((q) => q.status === 'uploading')

  return (
    <>
      <PageHeader title="Upload tradebooks" lede="Drop every broker's tradebook here. Each file is parsed, matched and computed in the background; progress shows live." />

      <div className="grid gap-6 lg:grid-cols-[minmax(0,1fr)_320px]">
        <div className="space-y-6">
          <Section title="Files">
            <div className="p-4 sm:p-5">
              <div
                {...getRootProps({
                  className: cn(
                    'flex flex-col items-center justify-center gap-3 rounded-lg border border-dashed border-rule-strong px-6 py-10 text-center transition-colors duration-150',
                    isDragActive && 'border-accent bg-accent-soft',
                  ),
                })}
              >
                <input {...getInputProps({ 'aria-label': 'Choose tradebook files' })} data-testid="file-input" />
                <UploadCloud className="size-6 text-muted" aria-hidden />
                <p className="text-sm">
                  {isDragActive ? 'Drop to add the files' : 'Drag tradebook files here'}
                  <span className="text-muted"> (CSV or XLSX, up to 20 MB each)</span>
                </p>
                <Button variant="secondary" size="sm" onClick={open}>
                  Choose files
                </Button>
              </div>

              <AnimatePresence initial={false}>
                {queue.length > 0 && (
                  <motion.ul variants={rise} initial="hidden" animate="visible" exit="exit" className="mt-4 divide-y divide-rule border-t border-rule" aria-label="Selected files">
                    {queue.map((q) => (
                      <QueuedRow key={q.key} item={q} onRemove={() => setQueue((list) => list.filter((x) => x.key !== q.key))} />
                    ))}
                  </motion.ul>
                )}
              </AnimatePresence>

              <div className="mt-4 flex flex-wrap items-center gap-3">
                <Button variant="primary" onClick={() => void startUploads()} disabled={queuedCount === 0 || !effectiveAccount || busy || needsBroker} data-testid="start-upload">
                  {busy ? 'Uploading' : queuedCount > 1 ? `Upload ${queuedCount} files` : 'Upload file'}
                </Button>
                {!effectiveAccount && accounts.isSuccess && <span className="text-sm text-muted">Create an account first.</span>}
                {needsBroker && <span className="text-sm text-warn">Pick the broker for XLSX files; they cannot be auto-detected.</span>}
              </div>
            </div>
          </Section>
        </div>

        <div className="space-y-6">
          <Section title="Account">
            <div className="space-y-4 p-4 sm:p-5">
              <Field label="Demat account" hint="Lots are matched FIFO within this account.">
                {({ id, describedBy }) => (
                  <Select value={effectiveAccount} onValueChange={setAccountId} disabled={!accounts.data?.items.length}>
                    <SelectTrigger id={id} aria-describedby={describedBy} data-testid="account-select">
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
              {showNewAccount || accounts.data?.items.length === 0 ? (
                <NewAccountForm
                  brokers={brokers.data?.items ?? []}
                  onCreated={(id) => {
                    setAccountId(id)
                    setShowNewAccount(false)
                  }}
                  onCancel={accounts.data?.items.length ? () => setShowNewAccount(false) : undefined}
                />
              ) : (
                <Button variant="link" size="sm" onClick={() => setShowNewAccount(true)}>
                  Add another account
                </Button>
              )}
            </div>
          </Section>

          <Section title="Broker">
            <div className="space-y-3 p-4 sm:p-5">
              <Field label="Broker format" hint="CSV files are detected from their columns. Override only if detection picks the wrong one.">
                {({ id, describedBy }) => (
                  <Select value={broker} onValueChange={setBroker}>
                    <SelectTrigger id={id} aria-describedby={describedBy} data-testid="broker-select">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value={AUTO}>Detect automatically</SelectItem>
                      {(brokers.data?.items ?? []).map((b) => (
                        <SelectItem key={b} value={b}>
                          {brokerLabel(b)}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                )}
              </Field>
            </div>
          </Section>

          <p className="text-[13px] text-muted">
            Raw files are deleted 24 hours after processing. Uploading the same file twice changes nothing. See <Link to="/dashboard" className="text-accent underline-offset-4 hover:underline">the dashboard</Link> for results.
          </p>
        </div>
      </div>
    </>
  )
}

function QueuedRow({ item, onRemove }: { item: QueuedFile; onRemove: () => void }) {
  const u = item.upload
  return (
    <li className="py-3" data-testid="queued-file" data-status={item.status}>
      <div className="flex items-start gap-3">
        <FileSpreadsheet className="mt-0.5 size-4 shrink-0 text-muted" aria-hidden />
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
            <span className="truncate text-sm font-medium">{item.file.name}</span>
            <span className="text-[13px] text-muted">{formatBytes(item.file.size)}</span>
            {u && (
              <span className="text-[13px] text-muted">
                {brokerLabel(u.broker)}
                {u.detect_confidence !== undefined && u.detect_confidence < 1 && ` (detected, ${Math.round(u.detect_confidence * 100)}% sure)`}
              </span>
            )}
            {item.status === 'uploading' && <Badge tone="accent">Uploading</Badge>}
            {item.status === 'error' && <Badge tone="loss">Failed</Badge>}
            {u?.duplicate && <Badge tone="neutral">Already uploaded, nothing changed</Badge>}
          </div>
          {item.status === 'error' && (
            <p role="alert" className="mt-1 text-[13px] text-loss">
              {item.error}
            </p>
          )}
          {u && !u.duplicate && u.job_id && (
            <div className="mt-2">
              <JobStepper jobId={u.job_id} label={item.file.name} />
            </div>
          )}
          {u?.duplicate && (
            <p className="mt-1 text-[13px] text-muted">
              This exact file was processed earlier.{' '}
              <Link to="/gains" className="text-accent underline-offset-4 hover:underline">
                See gains
              </Link>
            </p>
          )}
        </div>
        {item.status === 'queued' && (
          <Button variant="ghost" size="icon" className="size-7" onClick={onRemove} aria-label={`Remove ${item.file.name}`}>
            <X aria-hidden />
          </Button>
        )}
      </div>
    </li>
  )
}

const accountSchema = z.object({
  broker: z.string().min(1, 'Choose a broker'),
  nickname: z.string().trim().min(1, 'Give the account a name').max(60, 'Use at most 60 characters'),
})
type AccountValues = z.infer<typeof accountSchema>

export function NewAccountForm({ brokers, onCreated, onCancel }: { brokers: string[]; onCreated: (id: string) => void; onCancel?: () => void }) {
  const create = useCreateAccount()
  const formId = useId()
  const options = useMemo(() => [...brokers, 'other'], [brokers])
  const form = useForm<AccountValues>({ resolver: zodResolver(accountSchema), defaultValues: { broker: brokers[0] ?? '', nickname: '' } })
  const broker = form.watch('broker')

  const submit = form.handleSubmit((values) => {
    create.mutate(values, {
      onSuccess: (acc) => {
        form.reset({ broker: values.broker, nickname: '' })
        onCreated(acc.id)
      },
      onError: (e) => {
        if (isApiError(e)) {
          for (const [k, v] of Object.entries(e.errors)) if (k === 'broker' || k === 'nickname') form.setError(k, { type: 'server', message: v })
        }
      },
    })
  })

  const serverError = create.error && !(isApiError(create.error) && Object.keys(create.error.errors).length) ? errorMessage(create.error) : null

  return (
    <form id={formId} onSubmit={submit} noValidate className="space-y-3 rounded-md border border-rule bg-surface-2 p-3" aria-label="New demat account">
      <Field label="Broker" error={form.formState.errors.broker?.message}>
        {({ id, describedBy, invalid }) => (
          <Select value={broker} onValueChange={(v) => form.setValue('broker', v, { shouldValidate: true })}>
            <SelectTrigger id={id} aria-describedby={describedBy} aria-invalid={invalid} className="bg-surface" data-testid="new-account-broker">
              <SelectValue placeholder="Choose a broker" />
            </SelectTrigger>
            <SelectContent>
              {options.map((b) => (
                <SelectItem key={b} value={b}>
                  {brokerLabel(b)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        )}
      </Field>
      <Field label="Nickname" hint="For example, Zerodha main." error={form.formState.errors.nickname?.message}>
        {({ id, describedBy, invalid }) => <Input id={id} aria-describedby={describedBy} aria-invalid={invalid} data-testid="new-account-nickname" {...form.register('nickname')} />}
      </Field>
      <InlineError message={serverError} />
      <div className="flex gap-2">
        <Button type="submit" variant="primary" size="sm" disabled={create.isPending} data-testid="new-account-submit">
          {create.isPending ? 'Creating' : 'Create account'}
        </Button>
        {onCancel && (
          <Button variant="ghost" size="sm" onClick={onCancel}>
            Cancel
          </Button>
        )}
      </div>
    </form>
  )
}
