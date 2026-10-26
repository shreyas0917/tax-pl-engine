import { ChevronDown, LogOut, UserRound } from 'lucide-react'
import { NavLink, Outlet, useNavigate } from 'react-router'
import { useLogout, useMe, useSession } from '@/api/hooks'
import { Button } from '@/components/ui/button'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuSeparator, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { useYear } from '@/lib/year'
import { cn } from '@/lib/utils'
import { ThemeToggle } from './theme-toggle'
import { Wordmark } from './wordmark'

const NAV = [
  { to: '/dashboard', label: 'Dashboard' },
  { to: '/upload', label: 'Upload' },
  { to: '/review', label: 'Review' },
  { to: '/gains', label: 'Gains' },
  { to: '/trades', label: 'Trades' },
  { to: '/reports', label: 'Reports' },
  { to: '/settings', label: 'Settings' },
]

export function AppShell() {
  const session = useSession()
  const me = useMe(!!session)
  const openReview = me.data?.open_review_items ?? 0

  return (
    <div className="flex min-h-dvh flex-col md:flex-row">
      <a href="#main" className="sr-only focus:not-sr-only focus:fixed focus:left-3 focus:top-3 focus:z-[60] focus:rounded-md focus:bg-surface focus:px-3 focus:py-2 focus:text-sm">
        Skip to content
      </a>
      <aside className="hidden w-56 shrink-0 border-r border-rule bg-surface md:flex md:flex-col">
        <div className="px-5 py-5">
          <Wordmark />
        </div>
        <nav aria-label="Main" className="flex flex-1 flex-col gap-0.5 px-3">
          {NAV.map((n) => (
            <NavItem key={n.to} to={n.to} label={n.label} count={n.to === '/review' ? openReview : 0} />
          ))}
        </nav>
      </aside>

      <div className="flex min-w-0 flex-1 flex-col">
        <header className="sticky top-0 z-30 border-b border-rule bg-surface">
          <div className="flex h-14 items-center gap-3 px-4 sm:px-6">
            <div className="md:hidden">
              <Wordmark compact />
            </div>
            <div className="ml-auto flex min-w-0 items-center gap-2">
              <YearSelect />
              <ThemeToggle />
              <UserMenu />
            </div>
          </div>
          <nav aria-label="Main" className="flex gap-1 overflow-x-auto px-2 pb-1 md:hidden">
            {NAV.map((n) => (
              <NavItem key={n.to} to={n.to} label={n.label} count={n.to === '/review' ? openReview : 0} compact />
            ))}
          </nav>
        </header>
        <main id="main" className="mx-auto w-full max-w-[1200px] flex-1 px-4 py-6 sm:px-6 sm:py-8">
          <Outlet />
        </main>
        <Footer />
      </div>
    </div>
  )
}

function NavItem({ to, label, count, compact }: { to: string; label: string; count: number; compact?: boolean }) {
  return (
    <NavLink
      to={to}
      className={({ isActive }) =>
        cn(
          'flex items-center justify-between gap-2 rounded-md text-sm transition-colors duration-150',
          compact ? 'shrink-0 px-3 py-1.5' : 'px-2.5 py-1.5',
          isActive ? 'bg-accent-soft font-medium text-accent' : 'text-ink-2 hover:bg-surface-2 hover:text-ink',
        )
      }
    >
      <span>{label}</span>
      {count > 0 && (
        <span className="num rounded-sm bg-warn-soft px-1.5 text-[12px] font-medium text-warn" aria-label={`${count} open`}>
          {count}
        </span>
      )}
    </NavLink>
  )
}

function YearSelect() {
  const { fy, rules, rule, setFy } = useYear()
  if (!rules.length) return <div className="skeleton h-9 w-44" aria-hidden />
  return (
    <div className="flex items-center gap-2">
      <label htmlFor="year-select" className="sr-only">
        Tax year
      </label>
      <Select value={fy} onValueChange={(v) => setFy(v)}>
        <SelectTrigger id="year-select" className="w-auto min-w-[11rem]" data-testid="year-select">
          <SelectValue placeholder="Choose a year" />
        </SelectTrigger>
        <SelectContent align="end">
          {rules.map((r) => (
            <SelectItem key={r.year} value={r.year}>
              {r.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      {rule && <span className="hidden text-[13px] text-muted lg:inline">{rule.law}</span>}
    </div>
  )
}

function UserMenu() {
  const session = useSession()
  const logout = useLogout()
  const navigate = useNavigate()
  if (!session) return null
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="sm" className="max-w-[12rem] min-w-0" aria-label="Account menu">
          <UserRound aria-hidden className="sm:hidden" />
          <span className="hidden truncate sm:inline">{session.user.email}</span>
          <ChevronDown aria-hidden className="hidden sm:block" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        <DropdownMenuLabel>Signed in as {session.user.email}</DropdownMenuLabel>
        <DropdownMenuSeparator />
        <DropdownMenuItem onSelect={() => navigate('/settings')}>Settings</DropdownMenuItem>
        <DropdownMenuItem
          onSelect={() => {
            logout.mutate(undefined, { onSettled: () => navigate('/') })
          }}
        >
          <LogOut className="mr-2 size-4" aria-hidden /> Log out
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

export function Footer() {
  const { rule } = useYear()
  return (
    <footer className="border-t border-rule px-4 py-5 text-[13px] text-muted sm:px-6">
      <div className="mx-auto flex max-w-[1200px] flex-col gap-1 sm:flex-row sm:items-center sm:justify-between">
        <p>
          {rule ? `Rules version ${rule.rules_version}` : 'Rules version loading'}
          {rule && rule.verified_by === '' && '. Not yet signed off by a CA.'}
        </p>
        <p>This tool organises your trade data. It is not tax advice. Verify every figure with a qualified CA before filing.</p>
      </div>
    </footer>
  )
}
