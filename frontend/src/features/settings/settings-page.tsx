import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { useNavigate } from 'react-router'
import { z } from 'zod'
import { errorMessage, isApiError } from '@/api/client'
import { useAccounts, useBrokers, useChangePassword, useDeleteAccount, useDeleteMe, useLogout, useSession } from '@/api/hooks'
import { PageHeader, Section } from '@/components/layout/page'
import { Button } from '@/components/ui/button'
import { Field } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Dialog } from '@/components/ui/sheet'
import { Skeleton } from '@/components/ui/skeleton'
import { InlineError } from '@/components/ui/states'
import { NewAccountForm } from '@/features/upload/upload-page'
import { brokerLabel, formatDateTime } from '@/lib/format'
import { zodResolver } from '@/lib/zod-resolver'

export function SettingsPage() {
  const session = useSession()
  return (
    <>
      <PageHeader title="Settings" lede={session ? `Signed in as ${session.user.email}.` : undefined} />
      <div className="space-y-6">
        <AccountsSection />
        <PasswordSection />
        <DangerSection />
      </div>
    </>
  )
}

function AccountsSection() {
  const accounts = useAccounts()
  const brokers = useBrokers()
  const del = useDeleteAccount()
  const [adding, setAdding] = useState(false)
  const [confirm, setConfirm] = useState<{ id: string; name: string } | null>(null)

  return (
    <Section title="Demat accounts" aside={!adding && <Button variant="secondary" size="sm" onClick={() => setAdding(true)}>Add account</Button>}>
      {accounts.isLoading && (
        <div className="space-y-2 p-5">
          <Skeleton className="h-5 w-1/2" />
          <Skeleton className="h-5 w-1/3" />
        </div>
      )}
      {accounts.data && accounts.data.items.length === 0 && !adding && <p className="px-5 py-4 text-sm text-muted">No accounts yet. Add one to start uploading.</p>}
      {accounts.data && accounts.data.items.length > 0 && (
        <ul className="divide-y divide-rule">
          {accounts.data.items.map((a) => (
            <li key={a.id} className="flex flex-wrap items-center justify-between gap-3 px-5 py-3 text-sm">
              <span>
                <span className="font-medium">{a.nickname}</span> <span className="text-muted">{brokerLabel(a.broker)}, added {formatDateTime(a.created_at)}</span>
              </span>
              <Button variant="danger" size="sm" onClick={() => setConfirm({ id: a.id, name: a.nickname })}>
                Delete
              </Button>
            </li>
          ))}
        </ul>
      )}
      {adding && (
        <div className="border-t border-rule p-4 sm:p-5">
          <NewAccountForm brokers={brokers.data?.items ?? []} onCreated={() => setAdding(false)} onCancel={() => setAdding(false)} />
        </div>
      )}
      <Dialog open={!!confirm} onOpenChange={(o) => !o && setConfirm(null)} title={`Delete ${confirm?.name ?? 'account'}?`} description="Its uploads and trades go with it, and every year is recomputed.">
        {del.isError && <InlineError message={errorMessage(del.error)} />}
        <div className="mt-3 flex justify-end gap-2">
          <Button variant="secondary" onClick={() => setConfirm(null)}>
            Keep it
          </Button>
          <Button
            variant="danger"
            disabled={del.isPending}
            onClick={() => {
              if (confirm) del.mutate(confirm.id, { onSuccess: () => setConfirm(null) })
            }}
          >
            {del.isPending ? 'Deleting' : 'Delete account'}
          </Button>
        </div>
      </Dialog>
    </Section>
  )
}

const passwordSchema = z
  .object({
    current_password: z.string().min(1, 'Enter your current password'),
    new_password: z.string().min(10, 'Use at least 10 characters').max(200).regex(/[A-Za-z]/, 'Include at least one letter').regex(/[0-9]/, 'Include at least one digit'),
    confirm: z.string(),
  })
  .refine((v) => v.new_password === v.confirm, { path: ['confirm'], message: 'Passwords do not match' })
type PasswordValues = z.infer<typeof passwordSchema>

function PasswordSection() {
  const change = useChangePassword()
  const logout = useLogout()
  const navigate = useNavigate()
  const form = useForm<PasswordValues>({ resolver: zodResolver(passwordSchema), defaultValues: { current_password: '', new_password: '', confirm: '' } })
  const [done, setDone] = useState(false)

  const submit = form.handleSubmit((v) =>
    change.mutate(
      { current_password: v.current_password, new_password: v.new_password },
      {
        onSuccess: () => {
          setDone(true)
          form.reset()
          // The API revokes every refresh token; log out so the next refresh does not fail silently.
          setTimeout(() => logout.mutate(undefined, { onSettled: () => navigate('/login') }), 1500)
        },
        onError: (e) => {
          if (isApiError(e)) for (const [k, m] of Object.entries(e.errors)) if (k === 'current_password' || k === 'new_password') form.setError(k, { type: 'server', message: m })
        },
      },
    ),
  )

  return (
    <Section title="Password">
      <form onSubmit={submit} noValidate className="grid gap-4 p-4 sm:max-w-md sm:p-5">
        <Field label="Current password" error={form.formState.errors.current_password?.message}>
          {({ id, describedBy, invalid }) => <Input id={id} type="password" autoComplete="current-password" aria-describedby={describedBy} aria-invalid={invalid} {...form.register('current_password')} />}
        </Field>
        <Field label="New password" hint="Ten characters or more, with a letter and a digit." error={form.formState.errors.new_password?.message}>
          {({ id, describedBy, invalid }) => <Input id={id} type="password" autoComplete="new-password" aria-describedby={describedBy} aria-invalid={invalid} {...form.register('new_password')} />}
        </Field>
        <Field label="Repeat new password" error={form.formState.errors.confirm?.message}>
          {({ id, describedBy, invalid }) => <Input id={id} type="password" autoComplete="new-password" aria-describedby={describedBy} aria-invalid={invalid} {...form.register('confirm')} />}
        </Field>
        {change.isError && !(isApiError(change.error) && Object.keys(change.error.errors).length) && <InlineError message={errorMessage(change.error)} />}
        {done && (
          <p role="status" className="text-sm text-gain">
            Password changed. Signing you out so you can log in again.
          </p>
        )}
        <div>
          <Button type="submit" variant="primary" disabled={change.isPending || done}>
            {change.isPending ? 'Saving' : 'Change password'}
          </Button>
        </div>
      </form>
    </Section>
  )
}

const deleteSchema = z.object({
  password: z.string().min(1, 'Enter your password'),
  confirm: z.string().refine((v) => v.trim().toUpperCase() === 'DELETE', { message: 'Type DELETE to confirm' }),
})
type DeleteValues = z.infer<typeof deleteSchema>

function DangerSection() {
  const del = useDeleteMe()
  const navigate = useNavigate()
  const [open, setOpen] = useState(false)
  const form = useForm<DeleteValues>({ resolver: zodResolver(deleteSchema), defaultValues: { password: '', confirm: '' } })

  const submit = form.handleSubmit((v) =>
    del.mutate(
      { password: v.password, confirm: 'DELETE' },
      {
        onSuccess: () => navigate('/', { replace: true }),
        onError: (e) => {
          if (isApiError(e)) for (const [k, m] of Object.entries(e.errors)) if (k === 'password' || k === 'confirm') form.setError(k, { type: 'server', message: m })
        },
      },
    ),
  )

  return (
    <Section title="Delete all my data">
      <div className="flex flex-col gap-3 p-4 sm:flex-row sm:items-center sm:justify-between sm:p-5">
        <p className="max-w-prose text-sm text-muted">Removes your accounts, uploads, trades, lots, gains, review items, reports and sessions. This cannot be undone.</p>
        <Button variant="danger" onClick={() => setOpen(true)}>
          Delete everything
        </Button>
      </div>
      <Dialog open={open} onOpenChange={setOpen} title="Delete all your data?" description="Enter your password and type DELETE to confirm. Everything is removed immediately.">
        <form onSubmit={submit} noValidate className="space-y-4">
          <Field label="Password" error={form.formState.errors.password?.message}>
            {({ id, describedBy, invalid }) => <Input id={id} type="password" autoComplete="current-password" aria-describedby={describedBy} aria-invalid={invalid} {...form.register('password')} />}
          </Field>
          <Field label="Type DELETE to confirm" error={form.formState.errors.confirm?.message}>
            {({ id, describedBy, invalid }) => <Input id={id} autoComplete="off" aria-describedby={describedBy} aria-invalid={invalid} {...form.register('confirm')} />}
          </Field>
          {del.isError && !(isApiError(del.error) && Object.keys(del.error.errors).length) && <InlineError message={errorMessage(del.error)} />}
          <div className="flex justify-end gap-2">
            <Button variant="secondary" onClick={() => setOpen(false)}>
              Cancel
            </Button>
            <Button type="submit" variant="danger" disabled={del.isPending}>
              {del.isPending ? 'Deleting' : 'Delete all my data'}
            </Button>
          </div>
        </form>
      </Dialog>
    </Section>
  )
}
