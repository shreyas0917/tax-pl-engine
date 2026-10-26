import { useEffect, useRef, useState } from 'react'
import type { components } from './schema'
import { API_V1, api, refreshAccessToken, unwrap } from './client'
import { sessionStore } from './auth'

export type JobState = components['schemas']['JobState']
export type JobEvent = components['schemas']['JobEvent']

export const JOB_STAGES: JobState[] = ['UPLOADED', 'PARSING', 'NORMALIZED', 'ADJUSTED', 'MATCHED', 'COMPUTED', 'REPORT_READY']
export const TERMINAL_STATES: ReadonlySet<JobState> = new Set<JobState>(['REPORT_READY', 'NEEDS_REVIEW', 'FAILED'])

export function isTerminal(s: JobState | null | undefined): boolean {
  return !!s && TERMINAL_STATES.has(s)
}

export interface JobProgress {
  state: JobState | null
  events: JobEvent[]
  terminal: boolean
  error: string | null
  /** Job-level failure message from GET /jobs/{id} once terminal. */
  jobError: string | null
  connection: 'idle' | 'open' | 'closed' | 'error'
}

const initial: JobProgress = { state: null, events: [], terminal: false, error: null, jobError: null, connection: 'idle' }

/**
 * Follows a job over SSE (EventSource with the token in the query string, since
 * EventSource cannot set headers). On a connection error the token is refreshed
 * once and the stream reopened; on terminal state the job is fetched once more so
 * `jobError` carries the failure message.
 */
export function useJobProgress(jobId: string | null | undefined): JobProgress {
  const [progress, setProgress] = useState<JobProgress>(initial)
  const retried = useRef(false)

  useEffect(() => {
    if (!jobId) {
      setProgress(initial)
      return
    }
    let closed = false
    let source: EventSource | null = null
    retried.current = false
    setProgress({ ...initial, connection: 'idle' })

    const finish = async (state: JobState) => {
      try {
        const detail = await unwrap(api.GET('/jobs/{id}', { params: { path: { id: jobId } } }))
        setProgress((p) => ({
          ...p,
          state: detail.job.state,
          terminal: true,
          jobError: detail.job.error?.message ?? null,
          events: detail.events.length > p.events.length ? detail.events : p.events,
          connection: 'closed',
        }))
      } catch {
        setProgress((p) => ({ ...p, state, terminal: true, connection: 'closed' }))
      }
    }

    const open = (token: string) => {
      if (closed) return
      source = new EventSource(`${API_V1}/jobs/${jobId}/events?access_token=${encodeURIComponent(token)}`)
      source.addEventListener('open', () => setProgress((p) => ({ ...p, connection: 'open' })))
      source.addEventListener('state', (e: MessageEvent<string>) => {
        try {
          const ev = JSON.parse(e.data) as JobEvent
          setProgress((p) => {
            if (p.events.some((x) => x.id === ev.id)) return p
            return { ...p, state: ev.state, events: [...p.events, ev], terminal: isTerminal(ev.state) }
          })
        } catch {
          /* ignore malformed event */
        }
      })
      source.addEventListener('done', (e: MessageEvent<string>) => {
        source?.close()
        let state: JobState = 'REPORT_READY'
        try {
          state = (JSON.parse(e.data) as { state: JobState }).state
        } catch {
          /* ignore */
        }
        void finish(state)
      })
      source.addEventListener('error', () => {
        if (closed) return
        source?.close()
        if (!retried.current) {
          retried.current = true
          void refreshAccessToken().then((t) => {
            if (t) open(t)
            else setProgress((p) => ({ ...p, connection: 'error', error: 'Live progress disconnected.' }))
          })
        } else {
          // Fall back to polling the job once; the UI keeps the last known state.
          setProgress((p) => ({ ...p, connection: 'error', error: 'Live progress disconnected.' }))
          void pollUntilTerminal()
        }
      })
    }

    const pollUntilTerminal = async () => {
      for (let i = 0; i < 120 && !closed; i++) {
        try {
          const detail = await unwrap(api.GET('/jobs/{id}', { params: { path: { id: jobId } } }))
          setProgress((p) => ({ ...p, state: detail.job.state, events: detail.events, terminal: detail.job.terminal, jobError: detail.job.error?.message ?? null }))
          if (detail.job.terminal) return
        } catch {
          /* keep polling */
        }
        await new Promise((r) => setTimeout(r, 1500))
      }
    }

    const token = sessionStore.getToken()
    if (token) open(token)
    else void refreshAccessToken().then((t) => t && open(t))

    return () => {
      closed = true
      source?.close()
    }
  }, [jobId])

  return progress
}

export const STAGE_LABEL: Record<JobState, string> = {
  UPLOADED: 'Uploaded',
  PARSING: 'Parsing',
  NORMALIZED: 'Normalised',
  ADJUSTED: 'Corporate actions applied',
  MATCHED: 'Lots matched (FIFO)',
  COMPUTED: 'Gains computed',
  REPORT_READY: 'Ready',
  NEEDS_REVIEW: 'Needs your input',
  FAILED: 'Failed',
}
