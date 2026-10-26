import { beforeEach, describe, expect, it, vi } from 'vitest'
import { sessionStore } from './auth'
import { createAuthFetch } from './client'

const user = { id: 'u1', email: 'a@b.c', created_at: '2026-01-01T00:00:00Z' }

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { 'content-type': 'application/json' } })
}

describe('createAuthFetch', () => {
  beforeEach(() => {
    sessionStore.set({ accessToken: 'old', expiresAt: '2026-01-01T00:15:00Z', user })
  })

  it('attaches the bearer token', async () => {
    const base = vi.fn(async (req: Request) => {
      expect(req.headers.get('authorization')).toBe('Bearer old')
      return json({ ok: true })
    })
    const f = createAuthFetch({ baseFetch: base as unknown as typeof fetch, refreshUrl: 'http://api/api/v1/auth/refresh' })
    const res = await f('http://api/api/v1/me')
    expect(res.status).toBe(200)
    expect(base).toHaveBeenCalledTimes(1)
  })

  it('refreshes once on 401 and retries with the new token', async () => {
    const calls: string[] = []
    const base = vi.fn(async (req: Request) => {
      calls.push(`${req.method} ${req.url} ${req.headers.get('authorization') ?? ''}`)
      if (req.url.endsWith('/auth/refresh')) {
        expect(req.credentials).toBe('include')
        return json({ access_token: 'new', expires_at: '2026-01-01T00:30:00Z', user })
      }
      if (req.headers.get('authorization') === 'Bearer old') return json({ title: 'Unauthorized', status: 401 }, 401)
      return json({ ok: true })
    })
    const f = createAuthFetch({ baseFetch: base as unknown as typeof fetch, refreshUrl: 'http://api/api/v1/auth/refresh' })
    const res = await f('http://api/api/v1/summary?fy=2025-26')
    expect(res.status).toBe(200)
    expect(calls).toEqual([
      'GET http://api/api/v1/summary?fy=2025-26 Bearer old',
      'POST http://api/api/v1/auth/refresh ',
      'GET http://api/api/v1/summary?fy=2025-26 Bearer new',
    ])
    expect(sessionStore.getToken()).toBe('new')
  })

  it('clears the session and returns the 401 when refresh fails', async () => {
    const base = vi.fn(async (req: Request) => {
      if (req.url.endsWith('/auth/refresh')) return json({ title: 'Unauthorized', status: 401 }, 401)
      return json({ title: 'Unauthorized', status: 401 }, 401)
    })
    const f = createAuthFetch({ baseFetch: base as unknown as typeof fetch, refreshUrl: 'http://api/api/v1/auth/refresh' })
    const res = await f('http://api/api/v1/me')
    expect(res.status).toBe(401)
    expect(base).toHaveBeenCalledTimes(2)
    expect(sessionStore.get()).toBeNull()
  })

  it('does not try to refresh after a 401 from an auth route', async () => {
    const base = vi.fn(async () => json({ title: 'Unauthorized', status: 401 }, 401))
    const f = createAuthFetch({ baseFetch: base as unknown as typeof fetch, refreshUrl: 'http://api/api/v1/auth/refresh' })
    const res = await f('http://api/api/v1/auth/login', { method: 'POST', body: '{}' })
    expect(res.status).toBe(401)
    expect(base).toHaveBeenCalledTimes(1)
  })
})
