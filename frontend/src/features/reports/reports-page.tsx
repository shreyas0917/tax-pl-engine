import { Download } from 'lucide-react'
import { useState } from 'react'
import { downloadWithAuth, errorMessage } from '@/api/client'
import { useCreateReport, useReports, type ReportFormat } from '@/api/hooks'
import { JobStepper } from '@/components/job-stepper'
import { PageHeader, Section } from '@/components/layout/page'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { TableSkeleton } from '@/components/ui/skeleton'
import { EmptyState, ErrorState, InlineError } from '@/components/ui/states'
import { formatBytes, formatDateTime, shortHash } from '@/lib/format'
import { useYear } from '@/lib/year'

const FORMAT_LABEL: Record<ReportFormat, string> = { xlsx: 'Excel (.xlsx)', pdf: 'PDF', json: 'JSON' }

export function ReportsPage() {
  const { fy, rule } = useYear()
  const reports = useReports()
  const create = useCreateReport()
  const [format, setFormat] = useState<ReportFormat>('xlsx')
  const [job, setJob] = useState<string | null>(null)
  const [downloadError, setDownloadError] = useState<string | null>(null)
  const [downloading, setDownloading] = useState<string | null>(null)

  const generate = () => {
    setJob(null)
    create.mutate({ fy, format }, { onSuccess: (r) => setJob(r.job_id) })
  }

  const download = async (id: string, fallbackName: string) => {
    setDownloadError(null)
    setDownloading(id)
    try {
      const { blob, filename } = await downloadWithAuth(`/reports/${id}/download`)
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = filename ?? fallbackName
      document.body.appendChild(a)
      a.click()
      a.remove()
      setTimeout(() => URL.revokeObjectURL(url), 10_000)
    } catch (e) {
      setDownloadError(errorMessage(e))
    } finally {
      setDownloading(null)
    }
  }

  return (
    <>
      <PageHeader title="Reports" lede="Each report records the rules version and a checksum, so the same inputs always produce the same file." />

      <Section title={`Generate for ${rule?.label ?? fy}`} className="mb-6">
        <div className="flex flex-col gap-3 p-4 sm:flex-row sm:items-end sm:p-5">
          <div className="sm:w-56">
            <label htmlFor="report-format" className="block text-sm font-medium text-ink-2">
              Format
            </label>
            <Select value={format} onValueChange={(v) => setFormat(v as ReportFormat)}>
              <SelectTrigger id="report-format" className="mt-1.5" data-testid="report-format">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {(Object.keys(FORMAT_LABEL) as ReportFormat[]).map((f) => (
                  <SelectItem key={f} value={f}>
                    {FORMAT_LABEL[f]}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <Button variant="primary" onClick={generate} disabled={create.isPending} data-testid="generate-report">
            {create.isPending ? 'Queueing' : 'Generate report'}
          </Button>
        </div>
        {create.isError && (
          <div className="px-4 pb-4 sm:px-5">
            <InlineError message={errorMessage(create.error)} />
          </div>
        )}
        {job && (
          <div className="border-t border-rule px-4 py-4 sm:px-5">
            <JobStepper jobId={job} label="Report" onTerminal={() => void reports.refetch()} />
          </div>
        )}
      </Section>

      <Section title="Generated reports">
        {reports.isLoading && <TableSkeleton rows={4} cols={5} />}
        {reports.isError && <ErrorState message={errorMessage(reports.error)} onRetry={() => void reports.refetch()} />}
        {reports.data && reports.data.items.length === 0 && <EmptyState title="No reports yet" body="Generate one above. It takes a few seconds." />}
        {downloadError && (
          <div className="px-4 pt-4 sm:px-5">
            <InlineError message={downloadError} />
          </div>
        )}
        {reports.data && reports.data.items.length > 0 && (
          <div className="overflow-x-auto">
            <table className="w-full min-w-[760px] text-sm">
              <thead>
                <tr className="text-[13px] text-muted">
                  <th scope="col" className="px-3 py-2 pl-5 text-left font-medium">
                    Year
                  </th>
                  <th scope="col" className="px-3 py-2 text-left font-medium">
                    Format
                  </th>
                  <th scope="col" className="px-3 py-2 text-left font-medium">
                    Generated
                  </th>
                  <th scope="col" className="px-3 py-2 text-left font-medium">
                    Rules
                  </th>
                  <th scope="col" className="px-3 py-2 text-left font-medium">
                    Checksum (SHA-256)
                  </th>
                  <th scope="col" className="px-3 py-2 pr-5 text-right font-medium">
                    Size
                  </th>
                  <th scope="col" className="sr-only">
                    Download
                  </th>
                </tr>
              </thead>
              <tbody>
                {reports.data.items.map((r) => (
                  <tr key={r.id} className="border-t border-rule" data-testid="report-row">
                    <td className="px-3 py-2 pl-5 font-medium">{r.fy}</td>
                    <td className="px-3 py-2">
                      <Badge tone="outline">{r.format.toUpperCase()}</Badge>
                    </td>
                    <td className="px-3 py-2">{formatDateTime(r.created_at)}</td>
                    <td className="px-3 py-2">{r.rules_version}</td>
                    <td className="px-3 py-2">
                      <code className="num text-[12px]" title={r.checksum}>
                        {shortHash(r.checksum, 16)}
                      </code>
                    </td>
                    <td className="num px-3 py-2">{formatBytes(r.size_bytes)}</td>
                    <td className="px-3 py-2 pr-5 text-right">
                      <Button variant="secondary" size="sm" onClick={() => void download(r.id, `taxpl-${r.fy}.${r.format}`)} disabled={downloading === r.id} data-testid="download-report">
                        <Download aria-hidden /> {downloading === r.id ? 'Downloading' : 'Download'}
                      </Button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        <p className="border-t border-rule px-5 py-2.5 text-[13px] text-muted">Every report carries the not-tax-advice disclaimer. Share it with your CA; verify before filing.</p>
      </Section>
    </>
  )
}
