import { useForm } from 'react-hook-form'
import { Link, useLocation, useNavigate } from 'react-router'
import { z } from 'zod'
import { errorMessage, isApiError } from '@/api/client'
import { useLogin, useRegister } from '@/api/hooks'
import { Wordmark } from '@/components/layout/wordmark'
import { Button } from '@/components/ui/button'
import { Field } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { InlineError } from '@/components/ui/states'
import { zodResolver } from '@/lib/zod-resolver'

const schema = z.object({
  email: z.string().trim().min(1, 'Enter your email').email('Enter a valid email address').max(254),
  password: z
    .string()
    .min(10, 'Use at least 10 characters')
    .max(200, 'Use at most 200 characters')
    .regex(/[A-Za-z]/, 'Include at least one letter')
    .regex(/[0-9]/, 'Include at least one digit'),
})
type Values = z.infer<typeof schema>

function AuthForm({ mode }: { mode: 'login' | 'signup' }) {
  const navigate = useNavigate()
  const location = useLocation() as { state?: { from?: string } }
  const login = useLogin()
  const register = useRegister()
  const mutation = mode === 'login' ? login : register
  const form = useForm<Values>({ resolver: zodResolver(schema), defaultValues: { email: '', password: '' } })

  const onSubmit = form.handleSubmit((values) => {
    mutation.mutate(values, {
      onSuccess: () => navigate(location.state?.from ?? '/dashboard', { replace: true }),
      onError: (e) => {
        if (isApiError(e)) {
          for (const [field, message] of Object.entries(e.errors)) {
            if (field === 'email' || field === 'password') form.setError(field, { type: 'server', message })
          }
        }
      },
    })
  })

  const serverError = mutation.error && !(isApiError(mutation.error) && Object.keys(mutation.error.errors).length) ? errorMessage(mutation.error) : null

  return (
    <div className="flex min-h-dvh flex-col bg-paper">
      <header className="px-4 py-5 sm:px-6">
        <Wordmark to="/" />
      </header>
      <main className="mx-auto w-full max-w-sm flex-1 px-4 py-8 sm:px-0">
        <h1 className="text-2xl font-semibold">{mode === 'login' ? 'Log in' : 'Create your account'}</h1>
        <p className="mt-1 text-sm text-muted">
          {mode === 'login' ? 'Welcome back. Your tradebooks are where you left them.' : 'Ten characters or more, with a letter and a digit.'}
        </p>
        <form onSubmit={onSubmit} noValidate className="mt-6 space-y-4">
          <Field label="Email" error={form.formState.errors.email?.message}>
            {({ id, describedBy, invalid }) => (
              <Input id={id} type="email" autoComplete="email" aria-describedby={describedBy} aria-invalid={invalid} {...form.register('email')} />
            )}
          </Field>
          <Field label="Password" error={form.formState.errors.password?.message}>
            {({ id, describedBy, invalid }) => (
              <Input
                id={id}
                type="password"
                autoComplete={mode === 'login' ? 'current-password' : 'new-password'}
                aria-describedby={describedBy}
                aria-invalid={invalid}
                {...form.register('password')}
              />
            )}
          </Field>
          <InlineError message={serverError} />
          <Button type="submit" variant="primary" size="lg" className="w-full" disabled={mutation.isPending}>
            {mutation.isPending ? 'Please wait' : mode === 'login' ? 'Log in' : 'Create account'}
          </Button>
        </form>
        <p className="mt-6 text-sm text-muted">
          {mode === 'login' ? (
            <>
              New here?{' '}
              <Link to="/signup" className="text-accent underline-offset-4 hover:underline">
                Create an account
              </Link>
            </>
          ) : (
            <>
              Already have an account?{' '}
              <Link to="/login" className="text-accent underline-offset-4 hover:underline">
                Log in
              </Link>
            </>
          )}
        </p>
      </main>
      <footer className="px-4 py-5 text-[13px] text-muted sm:px-6">This tool organises your trade data. It is not tax advice.</footer>
    </div>
  )
}

export function LoginPage() {
  return <AuthForm mode="login" />
}

export function SignupPage() {
  return <AuthForm mode="signup" />
}
