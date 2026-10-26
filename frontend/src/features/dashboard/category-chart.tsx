import { Bar, BarChart, Cell, ReferenceLine, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts'
import type { Summary } from '@/api/hooks'
import { categoryLabel, formatSignedINR, signOf } from '@/lib/format'

type CategoryTotal = NonNullable<Summary['categories']>[number]

/**
 * Net gain per category: one series, bars coloured by polarity only.
 * Sign is also carried by the bar direction and the +/− label, so colour is never the only cue.
 */
export function CategoryChart({ categories }: { categories: CategoryTotal[] }) {
  const data = categories
    .filter((c) => c.category !== 'OUT_OF_SCOPE')
    .map((c) => ({ name: categoryLabel(c.category, true), raw: c.gain, value: Number(c.gain), count: c.count }))
  if (data.length === 0) return null
  const max = Math.max(1, ...data.map((d) => Math.abs(d.value)))

  return (
    <figure>
      <figcaption className="mb-2 text-[13px] text-muted">Net result by category</figcaption>
      <div className="h-[120px] w-full" role="img" aria-label={data.map((d) => `${d.name} ${formatSignedINR(d.raw, { whole: true })}`).join('; ')}>
        <ResponsiveContainer width="100%" height="100%">
          <BarChart data={data} layout="vertical" margin={{ top: 0, right: 8, bottom: 0, left: 0 }} barCategoryGap={4}>
            <XAxis type="number" hide domain={[-max * 1.35, max * 1.35]} />
            <YAxis type="category" dataKey="name" width={72} tickLine={false} axisLine={false} tick={{ fill: 'var(--muted)', fontSize: 12 }} />
            <ReferenceLine x={0} stroke="var(--rule-strong)" />
            <Tooltip
              cursor={{ fill: 'var(--surface-2)' }}
              content={({ active, payload }) => {
                const p = payload?.[0]?.payload as (typeof data)[number] | undefined
                if (!active || !p) return null
                return (
                  <div className="rounded-md border border-rule bg-surface px-2.5 py-1.5 text-[13px] text-ink">
                    <div className="font-medium">{p.name}</div>
                    <div className="num">{formatSignedINR(p.raw)}</div>
                    <div className="text-muted">{p.count} lines</div>
                  </div>
                )
              }}
            />
            <Bar dataKey="value" barSize={10} radius={[0, 4, 4, 0]} isAnimationActive={false} label={<SignedLabel />}>
              {data.map((d) => (
                <Cell key={d.name} fill={signOf(d.raw) === 'negative' ? 'var(--loss)' : 'var(--gain)'} />
              ))}
            </Bar>
          </BarChart>
        </ResponsiveContainer>
      </div>
      <table className="sr-only">
        <caption>Net result by category</caption>
        <thead>
          <tr>
            <th scope="col">Category</th>
            <th scope="col">Net</th>
          </tr>
        </thead>
        <tbody>
          {data.map((d) => (
            <tr key={d.name}>
              <td>{d.name}</td>
              <td>{formatSignedINR(d.raw)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </figure>
  )
}

function SignedLabel(props: { x?: number | string; y?: number | string; width?: number | string; height?: number | string; value?: number | string; index?: number }) {
  const { x = 0, y = 0, width = 0, height = 0, value } = props
  const n = Number(value)
  const w = Number(width)
  const xx = Number(x)
  const negative = n < 0
  const left = Math.min(xx, xx + w)
  const right = Math.max(xx, xx + w)
  const anchorX = negative ? left - 6 : right + 6
  return (
    <text x={anchorX} y={Number(y) + Number(height) / 2} dy={4} textAnchor={negative ? 'end' : 'start'} fontSize={12} fill="var(--ink-2)" className="num">
      {formatSignedINR(String(value ?? '0'), { whole: true })}
    </text>
  )
}
