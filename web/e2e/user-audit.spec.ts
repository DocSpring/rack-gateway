import type { Page } from '@playwright/test'
import { expect, test } from '@playwright/test'

async function login(page: Page) {
  await page.goto('/.gateway/web/login')
  const btn = page
    .getByTestId('login-cta')
    .or(page.getByRole('button', { name: /Continue with/i }))
    .or(page.getByRole('link', { name: /Continue with/i }))
  await expect(btn).toBeVisible({ timeout: 5000 })
  const navPromise = page.waitForURL(/oauth2\/v2\/auth|dev\/select-user/i)
  await btn.click()
  await navPromise

  const userCard = page.locator('text=Admin User')
  if (
    await userCard
      .first()
      .isVisible()
      .catch(() => false)
  ) {
    await userCard.first().click()
  }

  await page.waitForResponse((r) => r.url().includes('/.gateway/api/me') && r.status() === 200, {
    timeout: 20_000,
  })
}

function escapeRegex(value: string) {
  return value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

test.describe('User Audit Logs', () => {
  test('navigating from Users to user audit logs filters by that user', async ({ page }) => {
    await login(page)
    await page.goto('/.gateway/web/users')
    await expect(page.getByRole('heading', { name: 'Users' })).toBeVisible()

    // Click the first available audit link and capture its metadata
    const auditLink = page.locator('table tbody tr td a[href*="/users/"]').first()
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
      expect(auditResponse.ok()).toBeTruthy()
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
    // Expect an error message banner from the table pane
    await expect(page.getByText(/Failed to load audit logs/i)).toBeVisible()
  })
})
