import { expect, test } from '@playwright/test'
import { login } from './helpers'

function escapeRegex(value: string) {
  return value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

test.describe('User Audit Logs', () => {
  test('navigating from Users to user audit logs filters by that user', async ({ page }) => {
    await login(page, { userCardText: 'Deployer User' })
    await page.goto('/.gateway/web/users')
    await expect(page.getByRole('heading', { name: 'Users' })).toBeVisible()

    const targetEmail = 'deployer@example.com'

    const userRow = page.locator('table tbody tr', { hasText: targetEmail }).first()
    await expect(userRow).toBeVisible()

    // Click the user-specific audit link and capture its metadata
    const auditLink = userRow.locator('a[href*="/users/"]').first()
    await expect(auditLink).toBeVisible()

    const href = (await auditLink.getAttribute('href')) ?? ''
    const match = href.match(/\/users\/(\d+)\/audit_logs/)
    expect(match, 'user audit link should include user id').not.toBeNull()
    const userId = match ? match[1] : ''

    const { email: userEmail } = await auditLink.evaluate((node) => {
      const cell = node.closest('td')
      const anchors = cell ? Array.from(cell.querySelectorAll('a')) : []
      const emailAnchor = anchors.find((anchor) => (anchor.textContent || '').includes('@'))
      return { email: emailAnchor?.textContent?.trim() || null }
    })

    const auditRequestPromise = page.waitForRequest((req) => {
      if (!req.url().includes('/.gateway/api/admin/audit') || req.method() !== 'GET') {
        return false
      }
      try {
        const url = new URL(req.url())
        return url.searchParams.get('user_id') === userId
      } catch {
        return false
      }
    })

    const [auditRequest] = await Promise.all([
      auditRequestPromise,
      page.waitForURL(new RegExp(`/users/${userId}/audit_logs`)),
      auditLink.click(),
    ])

    const auditResponse = await auditRequest.response()
    if (auditResponse) {
      const status = auditResponse.status()
      if (status !== 200) {
        const body = await auditResponse.text().catch(() => '<unavailable>')
        throw new Error(
          `GET ${auditResponse.url()} expected 200, received ${status}. Response body:\n${body}`
        )
      }
    }

    await expect(page.getByRole('heading', { name: /Audit Logs/i })).toBeVisible()
    if (userEmail) {
      await expect(
        page.getByRole('heading', {
          name: new RegExp(`Audit Logs: ${escapeRegex(userEmail)}`, 'i'),
        })
      ).toBeVisible()
    }
  })

  test('invalid user id shows 404 error state', async ({ page }) => {
    await login(page)
    await page.goto('/.gateway/web/users/999999999/audit_logs')
    await expect(page.getByRole('heading', { name: /Audit Logs/i })).toBeVisible()
    await expect(page.getByText(/No audit logs found/i)).toBeVisible()
  })
})
