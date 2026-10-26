import { isRouteErrorResponse, Link, useRouteError } from 'react-router'
import { Button } from '@/components/ui/button'

export function RouteError() {
  const error = useRouteError()
  const message = isRouteErrorResponse(error) ? `${error.status} ${error.statusText}` : error instanceof Error ? error.message : 'Unexpected error'
  return (
    <div className="mx-auto max-w-md px-4 py-20">
      <h1 className="text-xl font-semibold">Something went wrong</h1>
      <p className="mt-2 text-sm text-muted">{message}</p>
      <div className="mt-5 flex gap-2">
        <Button variant="primary" onClick={() => window.location.reload()}>
          Reload
        </Button>
        <Button asChild variant="secondary">
          <Link to="/dashboard">Go to dashboard</Link>
        </Button>
      </div>
    </div>
  )
}
