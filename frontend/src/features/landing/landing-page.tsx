import { Link } from 'react-router'
import { useBrokers, useRules, useSession } from '@/api/hooks'
import { Wordmark } from '@/components/layout/wordmark'
import { ThemeToggle } from '@/components/layout/theme-toggle'
import { Button } from '@/components/ui/button'
import { brokerLabel } from '@/lib/format'

const STEPS = [
  {
    title: 'Upload each broker’s tradebook',
    body: 'CSV or XLSX from Zerodha, Groww, Upstox or Angel One. The broker is detected from the file; every rejected row is reported with its line number.',
  },
  {
    title: 'Every sale is matched to the shares it sold',
    body: 'FIFO per demat account, after splits, bonuses and demergers. Shares bought before 1 Feb 2018 get the grandfathered cost.',
  },
  {
    title: 'Read the explanation, then file',
    body: 'STCG, LTCG, intraday and F&O totals per year, with a why-this-number trail behind each line and an Excel, PDF or JSON report for your CA.',
  },
]

export function LandingPage() {
  const session = useSession()
  const brokers = useBrokers()
  const rules = useRules()
  const latest = rules.data?.items.at(-1)

  return (
    <div className="min-h-dvh bg-paper">
      <header className="mx-auto flex max-w-[1100px] items-center justify-between px-4 py-5 sm:px-6">
        <Wordmark to="/" />
        <div className="flex items-center gap-2">
          <ThemeToggle />
          {session ? (
            <Button asChild variant="primary" size="sm">
              <Link to="/dashboard">Open dashboard</Link>
            </Button>
          ) : (
            <>
              <Button asChild variant="ghost" size="sm">
                <Link to="/login">Log in</Link>
              </Button>
              <Button asChild variant="primary" size="sm">
                <Link to="/signup">Create account</Link>
              </Button>
            </>
          )}
        </div>
      </header>

      <main className="mx-auto max-w-[1100px] px-4 pb-20 sm:px-6">
        <section className="grid gap-10 py-10 sm:py-16 lg:grid-cols-[1.1fr_1fr] lg:items-center">
          <div>
            <h1 className="max-w-[18ch] text-[2rem] leading-[1.12] font-semibold sm:text-[2.6rem]">
              Your brokers each hand you a different tax report. Nobody adds them up.
            </h1>
            <p className="mt-5 max-w-prose text-[17px] text-ink-2">
              Tax P&amp;L Engine reads every tradebook, matches each sale to the lots it sold, FIFO per demat account, and shows the arithmetic behind every
              gain, so you and your CA can file with confidence.
            </p>
            <div className="mt-7 flex flex-wrap gap-3">
              <Button asChild variant="primary" size="lg">
                <Link to={session ? '/upload' : '/signup'}>Upload your first tradebook</Link>
              </Button>
              <Button asChild variant="secondary" size="lg">
                <Link to="/login">I already have an account</Link>
              </Button>
            </div>
          </div>
          <LedgerPreview />
        </section>

        <section aria-labelledby="how" className="border-t border-rule py-12">
          <h2 id="how" className="text-xl font-semibold">
            How it works
          </h2>
          <ol className="mt-6 grid gap-6 sm:grid-cols-3">
            {STEPS.map((s, i) => (
              <li key={s.title} className="relative border-t border-rule-strong pt-4">
                <span className="num text-sm text-muted">{i + 1}</span>
                <h3 className="mt-2 text-[15px] font-semibold">{s.title}</h3>
                <p className="mt-1.5 text-sm text-muted">{s.body}</p>
              </li>
            ))}
          </ol>
        </section>

        <section aria-labelledby="brokers" className="grid gap-8 border-t border-rule py-12 sm:grid-cols-2">
          <div>
            <h2 id="brokers" className="text-xl font-semibold">
              Supported brokers
            </h2>
            <ul className="mt-4 flex flex-wrap gap-2" aria-busy={brokers.isLoading}>
              {(brokers.data?.items ?? ['zerodha', 'groww', 'upstox', 'angelone']).map((b) => (
                <li key={b} className="rounded-md border border-rule bg-surface px-3 py-1.5 text-sm">
                  {brokerLabel(b)}
                </li>
              ))}
            </ul>
            <p className="mt-3 text-sm text-muted">Tradebook exports as CSV or XLSX. Contract-note charges can be imported later.</p>
          </div>
          <div>
            <h2 className="text-xl font-semibold">Your files stay yours</h2>
            <p className="mt-4 max-w-prose text-sm text-ink-2">
              Raw uploads are deleted 24 hours after processing; only the parsed trades remain. Your PAN is never stored in plain text. One click in settings
              deletes everything, including reports.
            </p>
            <p className="mt-3 text-sm text-muted">
              {latest ? `Rules version ${latest.rules_version}, ${latest.law}.` : 'Rules are versioned per year.'} This tool organises data; it is not tax
              advice.
            </p>
          </div>
        </section>
      </main>
    </div>
  )
}

/** A static, honest sketch of the product: one gain line and its trail. */
function LedgerPreview() {
  return (
    <div className="panel p-5 text-sm" aria-hidden>
      <div className="flex items-baseline justify-between">
        <span className="font-medium">INFY, sold 12 Aug 2025</span>
        <span className="text-muted">LTCG, 12.5%</span>
      </div>
      <ol className="mt-4 space-y-2 border-l border-rule-strong pl-4 text-ink-2">
        <li>50 shares sold at 1,520.00 in Zerodha</li>
        <li>Matched FIFO to 50 shares bought 3 Nov 2017</li>
        <li>Grandfathered: FMV on 31 Jan 2018 was higher than actual cost</li>
        <li>Held 2,839 days, long-term</li>
      </ol>
      <dl className="mt-5 grid grid-cols-[1fr_auto] gap-y-1">
        <dt className="text-muted">Sale value</dt>
        <dd className="num">₹76,000.00</dd>
        <dt className="text-muted">Cost of acquisition</dt>
        <dd className="num">−₹57,550.00</dd>
        <dt className="text-muted">Charges</dt>
        <dd className="num">−₹0.00</dd>
        <dt className="ledger-total mt-1 pt-1 font-medium">Gain</dt>
        <dd className="ledger-total num mt-1 pt-1 font-semibold text-gain">+₹18,450.00</dd>
      </dl>
    </div>
  )
}
