import { chromium } from 'playwright'

const OUT = process.env.OUT ?? new URL('.', import.meta.url).pathname
const APP = process.env.APP ?? 'http://localhost:5173'
const EMAIL = process.env.EMAIL ?? 'demo@example.com'
const PASSWORD = process.env.PASSWORD ?? 'demo-password-123'

const browser = await chromium.launch()
const page = await browser.newPage({ viewport: { width: 1280, height: 900 }, deviceScaleFactor: 2 })

const problems = []
page.on('console', (m) => m.type() === 'error' && problems.push(`console: ${m.text()}`))
page.on('requestfailed', (r) => problems.push(`request failed: ${r.method()} ${r.url()} — ${r.failure()?.errorText}`))
page.on('response', (r) => r.status() >= 400 && problems.push(`http ${r.status()}: ${r.request().method()} ${r.url()}`))

await page.goto(APP, { waitUntil: 'networkidle' })
await page.screenshot({ path: `${OUT}/01-signin.png`, fullPage: true })

await page.fill('input[type="email"]', EMAIL)
await page.fill('input[type="password"]', PASSWORD)
await page.click('button[type="submit"]')

await page.waitForSelector('.tiles', { timeout: 15000 })
await page.mouse.move(0, 0)
await page.waitForTimeout(1200)
await page.screenshot({ path: `${OUT}/02-dashboard.png`, fullPage: true })

await page.click('.theme-toggle button:nth-child(2)')
await page.waitForTimeout(500)
await page.screenshot({ path: `${OUT}/03-dashboard-dark.png`, fullPage: true })

await page.click('.theme-toggle button:nth-child(1)')
await page.setViewportSize({ width: 420, height: 900 })
await page.waitForTimeout(400)
await page.screenshot({ path: `${OUT}/04-mobile.png`, fullPage: true })

console.log(problems.length ? problems.join('\n') : 'no console or network errors')
await browser.close()
process.exit(problems.length ? 1 : 0)
