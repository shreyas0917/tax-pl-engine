/**
 * Session store: the bearer access token lives in memory, mirrored to
 * sessionStorage so a reload keeps the session until the tab closes.
 * The refresh token is an httpOnly cookie the browser sends on /auth/*.
 */
import type { components } from './schema'

export type SessionUser = components['schemas']['User']

export interface Session {
  accessToken: string
  expiresAt: string
  user: SessionUser
}

const STORAGE_KEY = 'taxpl.session'

type Listener = () => void

let session: Session | null = readStorage()
const listeners = new Set<Listener>()

function readStorage(): Session | null {
  try {
    const raw = sessionStorage.getItem(STORAGE_KEY)
    if (!raw) return null
    const parsed = JSON.parse(raw) as Partial<Session>
    if (parsed && typeof parsed.accessToken === 'string' && parsed.user) return parsed as Session
  } catch {
    /* ignore */
  }
  return null
}

function writeStorage(value: Session | null) {
  try {
    if (value) sessionStorage.setItem(STORAGE_KEY, JSON.stringify(value))
    else sessionStorage.removeItem(STORAGE_KEY)
  } catch {
    /* ignore */
  }
}

function emit() {
  for (const l of listeners) l()
}

export const sessionStore = {
  get(): Session | null {
    return session
  },
  getToken(): string | null {
    return session?.accessToken ?? null
  },
  set(next: Session | null) {
    session = next
    writeStorage(next)
    emit()
  },
  clear() {
    sessionStore.set(null)
  },
  subscribe(listener: Listener): () => void {
    listeners.add(listener)
    return () => listeners.delete(listener)
  },
}

export function sessionFromToken(t: components['schemas']['TokenResponse']): Session {
  return { accessToken: t.access_token, expiresAt: t.expires_at, user: t.user }
}
