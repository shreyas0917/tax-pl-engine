import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useSyncExternalStore } from 'react'
import type { components } from './schema'
import { api, unwrap, uploadFile } from './client'
import { sessionFromToken, sessionStore, type Session } from './auth'

export type Rule = components['schemas']['Rule']
export type Account = components['schemas']['Account']
export type Summary = components['schemas']['Summary']
export type SummaryResponse = components['schemas']['SummaryResponse']
export type Upload = components['schemas']['Upload']
export type ReviewItem = components['schemas']['ReviewItem']
export type Gain = components['schemas']['Gain']
export type GainPage = components['schemas']['GainPage']
export type ExplainTree = components['schemas']['ExplainTree']
export type Trade = components['schemas']['Trade']
export type TradePage = components['schemas']['TradePage']
export type Holding = components['schemas']['Holding']
export type Report = components['schemas']['Report']
export type Category = components['schemas']['Category']
export type ReportFormat = components['schemas']['ReportFormat']
export type Credentials = components['schemas']['Credentials']

export const queryKeys = {
  rules: ['rules'] as const,
  brokers: ['brokers'] as const,
  me: ['me'] as const,
  accounts: ['accounts'] as const,
  summary: (fy: string) => ['summary', fy] as const,
  uploads: ['uploads'] as const,
  reviewItems: (status?: string) => ['review-items', status ?? 'all'] as const,
  gains: (f: GainFilters) => ['gains', f] as const,
  explain: (id: string) => ['explain', id] as const,
  trades: (f: TradeFilters) => ['trades', f] as const,
  holdings: ['holdings'] as const,
  reports: ['reports'] as const,
  jobs: ['jobs'] as const,
}

export function useSession(): Session | null {
  return useSyncExternalStore(sessionStore.subscribe, sessionStore.get, sessionStore.get)
}

export function useRules() {
  return useQuery({ queryKey: queryKeys.rules, queryFn: () => unwrap(api.GET('/rules')), staleTime: Infinity })
}

export function useBrokers() {
  return useQuery({ queryKey: queryKeys.brokers, queryFn: () => unwrap(api.GET('/brokers')), staleTime: Infinity })
}

export function useMe(enabled = true) {
  return useQuery({ queryKey: queryKeys.me, queryFn: () => unwrap(api.GET('/me')), enabled })
}

export function useAccounts() {
  return useQuery({ queryKey: queryKeys.accounts, queryFn: () => unwrap(api.GET('/accounts')) })
}

export function useSummary(fy: string | null) {
  return useQuery({
    queryKey: queryKeys.summary(fy ?? ''),
    queryFn: () => unwrap(api.GET('/summary', { params: { query: { fy: fy ?? '' } } })),
    enabled: !!fy,
  })
}

export function useUploads() {
  return useQuery({ queryKey: queryKeys.uploads, queryFn: () => unwrap(api.GET('/uploads')) })
}

export function useReviewItems(status: 'open' | 'resolved' | 'dismissed' | undefined = 'open') {
  return useQuery({
    queryKey: queryKeys.reviewItems(status),
    queryFn: () => unwrap(api.GET('/review-items', { params: { query: status ? { status } : {} } })),
  })
}

export interface GainFilters {
  fy: string
  category?: Category
  account?: string
  isin?: string
}

export function useGains(filters: GainFilters) {
  return useInfiniteQuery({
    queryKey: queryKeys.gains(filters),
    queryFn: ({ pageParam }) =>
      unwrap(
        api.GET('/gains', {
          params: {
            query: {
              fy: filters.fy,
              category: filters.category,
              account: filters.account || undefined,
              isin: filters.isin || undefined,
              cursor: pageParam || undefined,
              limit: 200,
            },
          },
        }),
      ),
    initialPageParam: '',
    getNextPageParam: (last: GainPage) => (last.next_cursor ? last.next_cursor : undefined),
    enabled: !!filters.fy,
  })
}

export function useExplain(gainId: string | null) {
  return useQuery({
    queryKey: queryKeys.explain(gainId ?? ''),
    queryFn: () => unwrap(api.GET('/gains/{id}/explain', { params: { path: { id: gainId ?? '' } } })),
    enabled: !!gainId,
  })
}

export interface TradeFilters {
  fy?: string
  account?: string
  isin?: string
}

export function useTrades(filters: TradeFilters) {
  return useInfiniteQuery({
    queryKey: queryKeys.trades(filters),
    queryFn: ({ pageParam }) =>
      unwrap(
        api.GET('/trades', {
          params: {
            query: {
              fy: filters.fy || undefined,
              account: filters.account || undefined,
              isin: filters.isin || undefined,
              cursor: pageParam || undefined,
              limit: 200,
            },
          },
        }),
      ),
    initialPageParam: '',
    getNextPageParam: (last: TradePage) => (last.next_cursor ? last.next_cursor : undefined),
  })
}

export function useHoldings() {
  return useQuery({ queryKey: queryKeys.holdings, queryFn: () => unwrap(api.GET('/holdings')) })
}

export function useReports() {
  return useQuery({ queryKey: queryKeys.reports, queryFn: () => unwrap(api.GET('/reports')) })
}

/** Invalidate everything derived from trades after a job finishes. */
export function useInvalidateDerived() {
  const qc = useQueryClient()
  return () => {
    void qc.invalidateQueries({ queryKey: ['summary'] })
    void qc.invalidateQueries({ queryKey: ['gains'] })
    void qc.invalidateQueries({ queryKey: ['trades'] })
    void qc.invalidateQueries({ queryKey: queryKeys.holdings })
    void qc.invalidateQueries({ queryKey: ['review-items'] })
    void qc.invalidateQueries({ queryKey: queryKeys.uploads })
    void qc.invalidateQueries({ queryKey: queryKeys.me })
    void qc.invalidateQueries({ queryKey: queryKeys.reports })
  }
}

export function useLogin() {
  return useMutation({
    mutationFn: async (body: Credentials) => {
      const t = await unwrap(api.POST('/auth/login', { body }))
      sessionStore.set(sessionFromToken(t))
      return t
    },
  })
}

export function useRegister() {
  return useMutation({
    mutationFn: async (body: Credentials) => {
      const t = await unwrap(api.POST('/auth/register', { body }))
      sessionStore.set(sessionFromToken(t))
      return t
    },
  })
}

export function useLogout() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async () => {
      try {
        await api.POST('/auth/logout')
      } finally {
        sessionStore.clear()
        qc.clear()
      }
    },
  })
}

export function useCreateAccount() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: components['schemas']['CreateAccountRequest']) => unwrap(api.POST('/accounts', { body })),
    onSuccess: () => void qc.invalidateQueries({ queryKey: queryKeys.accounts }),
  })
}

export function useDeleteAccount() {
  const qc = useQueryClient()
  const invalidate = useInvalidateDerived()
  return useMutation({
    mutationFn: (id: string) => unwrap(api.DELETE('/accounts/{id}', { params: { path: { id } } })),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: queryKeys.accounts })
      invalidate()
    },
  })
}

export function useUpload() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ file, accountId, broker }: { file: File; accountId: string; broker?: string }) => uploadFile(file, accountId, broker),
    onSuccess: () => void qc.invalidateQueries({ queryKey: queryKeys.uploads }),
  })
}

export function useResolveReviewItem() {
  return useMutation({
    mutationFn: ({ id, body }: { id: string; body: components['schemas']['ResolveReviewItemRequest'] }) =>
      unwrap(api.POST('/review-items/{id}/resolve', { params: { path: { id } }, body })),
  })
}

export function useCreateReport() {
  return useMutation({
    mutationFn: (body: components['schemas']['CreateReportRequest']) => unwrap(api.POST('/reports', { body })),
  })
}

export function useChangePassword() {
  return useMutation({
    mutationFn: (body: components['schemas']['ChangePasswordRequest']) => unwrap(api.POST('/me/password', { body })),
  })
}

export function useDeleteMe() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: components['schemas']['DeleteMeRequest']) => unwrap(api.DELETE('/me', { body })),
    onSuccess: () => {
      sessionStore.clear()
      qc.clear()
    },
  })
}
