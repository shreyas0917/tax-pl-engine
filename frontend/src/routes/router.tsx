import { createBrowserRouter, Navigate, Outlet, useLocation } from 'react-router'
import { useSession } from '@/api/hooks'
import { AppShell } from '@/components/layout/app-shell'
import { RouteError } from './route-error'

function RequireAuth() {
  const session = useSession()
  const location = useLocation()
  if (!session) return <Navigate to="/login" replace state={{ from: location.pathname }} />
  return <Outlet />
}

function PublicOnly() {
  const session = useSession()
  if (session) return <Navigate to="/dashboard" replace />
  return <Outlet />
}

export const router = createBrowserRouter([
  {
    errorElement: <RouteError />,
    children: [
      { path: '/', lazy: () => import('@/features/landing/landing-page').then((m) => ({ Component: m.LandingPage })) },
      {
        element: <PublicOnly />,
        children: [
          { path: '/login', lazy: () => import('@/features/auth/auth-page').then((m) => ({ Component: m.LoginPage })) },
          { path: '/signup', lazy: () => import('@/features/auth/auth-page').then((m) => ({ Component: m.SignupPage })) },
        ],
      },
      {
        element: <RequireAuth />,
        children: [
          {
            element: <AppShell />,
            children: [
              { path: '/dashboard', lazy: () => import('@/features/dashboard/dashboard-page').then((m) => ({ Component: m.DashboardPage })) },
              { path: '/upload', lazy: () => import('@/features/upload/upload-page').then((m) => ({ Component: m.UploadPage })) },
              { path: '/review', lazy: () => import('@/features/review/review-page').then((m) => ({ Component: m.ReviewPage })) },
              { path: '/gains', lazy: () => import('@/features/gains/gains-page').then((m) => ({ Component: m.GainsPage })) },
              { path: '/trades', lazy: () => import('@/features/trades/trades-page').then((m) => ({ Component: m.TradesPage })) },
              { path: '/holdings', lazy: () => import('@/features/trades/trades-page').then((m) => ({ Component: m.HoldingsPage })) },
              { path: '/reports', lazy: () => import('@/features/reports/reports-page').then((m) => ({ Component: m.ReportsPage })) },
              { path: '/settings', lazy: () => import('@/features/settings/settings-page').then((m) => ({ Component: m.SettingsPage })) },
            ],
          },
        ],
      },
      { path: '*', element: <Navigate to="/" replace /> },
    ],
  },
])
