import { createContext, useCallback, useContext, useMemo, useState, type ReactNode } from 'react'
import { useRules, type Rule } from '@/api/hooks'

const FY_KEY = 'taxpl.fy'

interface YearContextValue {
  fy: string
  /** True until the user has picked a year explicitly (lets the dashboard auto-select a year with data). */
  auto: boolean
  rule: Rule | undefined
  rules: Rule[]
  setFy: (fy: string, explicit?: boolean) => void
}

const YearContext = createContext<YearContextValue | null>(null)

/** Indian financial year label for a date: 1 Apr 2026 -> "2026-27". */
export function financialYearOf(d: Date): string {
  const y = d.getMonth() >= 3 ? d.getFullYear() : d.getFullYear() - 1
  return `${y}-${String((y + 1) % 100).padStart(2, '0')}`
}

export function YearProvider({ children }: { children: ReactNode }) {
  const { data } = useRules()
  const rules = useMemo(() => data?.items ?? [], [data])
  const [stored] = useState<string | null>(() => {
    try {
      return localStorage.getItem(FY_KEY)
    } catch {
      return null
    }
  })
  const [chosen, setFyState] = useState<string>(stored ?? financialYearOf(new Date()))
  const [auto, setAuto] = useState(stored === null)

  // If the chosen year has no rules file, show the latest available instead.
  const fy = rules.length && !rules.some((r) => r.year === chosen) ? (rules[rules.length - 1]?.year ?? chosen) : chosen

  const setFy = useCallback((next: string, explicit = true) => {
    setFyState(next)
    if (explicit) setAuto(false)
    try {
      localStorage.setItem(FY_KEY, next)
    } catch {
      /* ignore */
    }
  }, [])

  const value = useMemo<YearContextValue>(() => ({ fy, auto, rule: rules.find((r) => r.year === fy), rules, setFy }), [fy, auto, rules, setFy])
  return <YearContext.Provider value={value}>{children}</YearContext.Provider>
}

export function useYear(): YearContextValue {
  const ctx = useContext(YearContext)
  if (!ctx) throw new Error('useYear must be used inside YearProvider')
  return ctx
}
