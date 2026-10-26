import createClient from 'openapi-fetch'
import type { components, paths } from './schema'
import { sessionFromToken, sessionStore } from './auth'

export const API_BASE: string = (import.meta.env.VITE_API_URL as string | undefined)?.replace(/\/$/, '') ?? 'http://localhost:8080'
export const API_V1 = `${API_BASE}/api/v1`

export type Problem = components['schemas']['Problem']

/** A failed API call carrying the RFC 9457 problem document when there was one. */
export class ApiError extends Error {
  readonly status: number
  readonly problem: Problem | null
  readonly errors: Record<string, string>

  constructor(status: number, problem: Problem | null, fallback?: string) {
    super(problem?.detail || problem?.title || fallback || `Request failed (${status})`)
    this.name = 'ApiError'
    this.status = status
    this.problem = problem
    this.errors = problem?.errors ?? {}
  }
}

export function isApiError(e: unknown): e is ApiError {
  return e instanceof ApiError
}

export function errorMessage(e: unknown): string {
  if (isApiError(e)) return e.message
  if (e instanceof TypeError) return 'Could not reach the server. Check your connection and try again.'
  if (e instanceof Error) return e.message
  return 'Something went wrong.'
}

export async function problemFromResponse(response: Response): Promise<Problem | null> {
  const ct = response.headers.get('content-type') ?? ''
  if (!ct.includes('json')) return null
  try {
    return (await response.clone().json()) as Problem
  } catch {
    return null
  }
}

type RefreshResult = components['schemas']['TokenResponse']

export interface AuthFetchOptions {
  baseFetch?: typeof fetch
  refreshUrl?: string
}

let refreshInFlight: Promise<string | null> | null = null

/**
 * Refresh the access token with the httpOnly cookie. Single-flight: concurrent
 * callers share one request. Resolves to the new token, or null when the
 * session is gone (the store is cleared in that case).
 */
export function refreshAccessToken(baseFetch: typeof fetch = fetch, refreshUrl = `${API_V1}/auth/refresh`): Promise<string | null> {
  if (refreshInFlight) return refreshInFlight
  refreshInFlight = (async () => {
    try {
      const res = await baseFetch(new Request(refreshUrl, { method: 'POST', credentials: 'include' }))
      if (!res.ok) {
        sessionStore.clear()
        return null
      }
      const body = (await res.json()) as RefreshResult
      sessionStore.set(sessionFromToken(body))
      return body.access_token
    } catch {
      return null
    } finally {
      refreshInFlight = null
    }
  })()
  return refreshInFlight
}

function isAuthRoute(url: string): boolean {
  return /\/api\/v1\/auth\//.test(url)
}

/**
 * fetch wrapper used by the typed client: attaches the bearer token, and on a
 * 401 from a non-auth route refreshes once and retries the same request.
 */
export function createAuthFetch(opts: AuthFetchOptions = {}): typeof fetch {
  const baseFetch = opts.baseFetch ?? fetch
  const refreshUrl = opts.refreshUrl ?? `${API_V1}/auth/refresh`

  return async (input, init) => {
    const request = new Request(input, init)
    const url = request.url
    const auth = isAuthRoute(url)
    const send = (token: string | null) => {
      const headers = new Headers(request.headers)
      if (token && !auth) headers.set('Authorization', `Bearer ${token}`)
      const init: RequestInit & { duplex?: 'half' } = { headers, credentials: auth ? 'include' : 'same-origin', duplex: 'half' }
      return baseFetch(new Request(request.clone(), init))
    }

    const first = await send(sessionStore.getToken())
    if (first.status !== 401 || auth) return first

    const token = await refreshAccessToken(baseFetch, refreshUrl)
    if (!token) return first
    return send(token)
  }
}

export const api = createClient<paths>({
  baseUrl: API_V1,
  fetch: createAuthFetch(),
})

/** Turn an openapi-fetch result into data-or-throw. */
export async function unwrap<T>(p: Promise<{ data?: T; error?: unknown; response: Response }>): Promise<T> {
  const { data, error, response } = await p
  if (error !== undefined || !response.ok) {
    const problem = (error && typeof error === 'object' && 'status' in error ? (error as Problem) : null) ?? (await problemFromResponse(response))
    throw new ApiError(response.status, problem)
  }
  return data as T
}

/** Authenticated download helper (reports). Returns the blob and the suggested filename. */
export async function downloadWithAuth(path: string): Promise<{ blob: Blob; filename: string | null; checksum: string | null }> {
  const f = createAuthFetch()
  const res = await f(`${API_V1}${path}`, { method: 'GET' })
  if (!res.ok) throw new ApiError(res.status, await problemFromResponse(res))
  const cd = res.headers.get('content-disposition') ?? ''
  const m = /filename="?([^";]+)"?/.exec(cd)
  return { blob: await res.blob(), filename: m?.[1] ?? null, checksum: res.headers.get('x-checksum-sha256') }
}

/** Multipart upload (openapi-fetch cannot build FormData from the typed body). */
export async function uploadFile(file: File, accountId: string, broker?: string): Promise<components['schemas']['Upload']> {
  const form = new FormData()
  form.append('file', file, file.name)
  form.append('account_id', accountId)
  if (broker) form.append('broker', broker)
  const f = createAuthFetch()
  const res = await f(`${API_V1}/uploads`, { method: 'POST', body: form })
  if (!res.ok) throw new ApiError(res.status, await problemFromResponse(res))
  return (await res.json()) as components['schemas']['Upload']
}
