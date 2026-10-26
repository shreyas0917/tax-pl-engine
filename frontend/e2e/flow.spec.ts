import { expect, test, type Page } from '@playwright/test'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const here = path.dirname(fileURLToPath(import.meta.url))

const SEED = path.resolve(here, '../../.data/seed')
const TRADEBOOK = path.join(SEED, 'zerodha_account1_tradebook.csv')
const FNO = path.join(SEED, 'zerodha_account1_fno.csv')
const SHOTS = path.join(here, 'screenshots')

const email = `e2e-${Date.now()}-${Math.random().toString(36).slice(2, 8)}@example.com`
const password = 'e2e-password-42'

async function setTheme(page: Page, theme: 'light' | 'dark') {
  await page.evaluate((t) => {
    localStorage.setItem('taxpl.theme', t)
    document.documentElement.dataset.theme = t
  }, theme)
}

test.describe.configure({ mode: 'serial' })

test('register, upload two files, explain a gain, review, report', async ({ page }) => {
  test.setTimeout(300_000)

  // Register
  await page.goto('/signup')
  await page.getByLabel('Email').fill(email)
  await page.getByLabel('Password').fill(password)
  await page.getByRole('button', { name: 'Create account' }).click()
  await expect(page).toHaveURL(/\/dashboard/)
  await expect(page.getByRole('heading', { name: 'Dashboard' })).toBeVisible()
  await expect(page.getByText('Upload your first tradebook')).toBeVisible()

  // Create an account on the upload page
  await page.goto('/upload')
  await page.getByTestId('new-account-nickname').fill('Zerodha main')
  await page.getByTestId('new-account-submit').click()
  await expect(page.getByTestId('account-select')).toContainText('Zerodha main')

  // Upload both files
  await page.getByTestId('file-input').setInputFiles([TRADEBOOK, FNO])
  await expect(page.getByTestId('queued-file')).toHaveCount(2)
  await page.getByTestId('start-upload').click()

  const steppers = page.getByTestId('job-stepper')
  await expect(steppers).toHaveCount(2, { timeout: 60_000 })
  for (const i of [0, 1]) {
    await expect
      .poll(async () => steppers.nth(i).getAttribute('data-state'), { timeout: 180_000, message: `job ${i} terminal` })
      .toMatch(/REPORT_READY|NEEDS_REVIEW|FAILED/)
  }
  const states = await Promise.all([0, 1].map((i) => steppers.nth(i).getAttribute('data-state')))
  expect(states).not.toContain('FAILED')

  // Gains: the seed data lives in earlier years; the dashboard auto-picks a year with data.
  await page.goto('/dashboard')
  await expect(page.getByTestId('figure-STCG_EQUITY')).toBeVisible({ timeout: 30_000 })
  await setTheme(page, 'light')
  await page.screenshot({ path: path.join(SHOTS, 'dashboard-light.png'), fullPage: true })
  await setTheme(page, 'dark')
  await page.waitForTimeout(300)
  await page.screenshot({ path: path.join(SHOTS, 'dashboard-dark.png'), fullPage: true })
  await setTheme(page, 'light')

  await page.goto('/gains')
  const rows = page.getByTestId('gain-row')
  await expect(rows.first()).toBeVisible({ timeout: 30_000 })
  await page.waitForTimeout(400)
  await page.screenshot({ path: path.join(SHOTS, 'gains.png'), fullPage: true })

  // Explain drawer on the first row
  await rows.first().click()
  const dialog = page.getByRole('dialog')
  await expect(dialog).toBeVisible()
  await expect(page.getByTestId('explain-body')).toBeVisible({ timeout: 20_000 })
  await expect(dialog.getByText('Rules version')).toBeVisible()
  await page.waitForTimeout(500)
  await page.screenshot({ path: path.join(SHOTS, 'explain-drawer.png'), fullPage: false })
  await page.keyboard.press('Escape')
  await expect(dialog).toBeHidden()

  // Review: resolve a MISSING_BUY_HISTORY item if there is one
  await page.goto('/review')
  await expect(page.getByRole('heading', { name: 'Review' })).toBeVisible()
  await page.waitForLoadState('networkidle')
  const missingBuy = page.locator('[data-testid="review-item"][data-kind="MISSING_BUY_HISTORY"]').first()
  if (await missingBuy.count()) {
    await missingBuy.getByTestId('acquisition-date').fill('2020-01-15')
    await missingBuy.getByTestId('cost-total').fill('1000')
    await missingBuy.getByTestId('resolve-submit').click()
    const stepper = page.getByTestId('job-stepper').first()
    await expect(stepper).toBeVisible({ timeout: 30_000 })
    await expect.poll(async () => stepper.getAttribute('data-state'), { timeout: 180_000 }).toMatch(/REPORT_READY|NEEDS_REVIEW|FAILED/)
    expect(await stepper.getAttribute('data-state')).not.toBe('FAILED')
  } else {
    test.info().annotations.push({ type: 'note', description: 'No MISSING_BUY_HISTORY review item was raised for the seed data.' })
  }

  // Reports: generate xlsx (fall back to json on 422), then download.
  await page.goto('/reports')
  await page.getByTestId('generate-report').click()
  const reportStepper = page.getByTestId('job-stepper').first()
  const failed = page.getByRole('alert').filter({ hasText: /422|not (yet )?(supported|available|deployed)/i })
  let format = 'xlsx'
  await Promise.race([reportStepper.waitFor({ timeout: 30_000 }), failed.waitFor({ timeout: 30_000 }).catch(() => {})])
  if (!(await reportStepper.count())) {
    test.info().annotations.push({ type: 'note', description: 'xlsx returned an error; falling back to json.' })
    format = 'json'
    await page.getByTestId('report-format').click()
    await page.getByRole('option', { name: 'JSON' }).click()
    await page.getByTestId('generate-report').click()
    await expect(reportStepper).toBeVisible({ timeout: 30_000 })
  }
  await expect.poll(async () => reportStepper.getAttribute('data-state'), { timeout: 180_000 }).toMatch(/REPORT_READY|FAILED/)
  expect(await reportStepper.getAttribute('data-state')).toBe('REPORT_READY')

  const row = page.getByTestId('report-row').first()
  await expect(row).toBeVisible({ timeout: 30_000 })
  await expect(row).toContainText(format.toUpperCase())
  const downloadPromise = page.waitForEvent('download', { timeout: 30_000 })
  await row.getByTestId('download-report').click()
  const download = await downloadPromise
  expect(download.suggestedFilename()).toMatch(new RegExp(`\\.${format}$`))
  const file = await download.path()
  expect(file).toBeTruthy()
})
