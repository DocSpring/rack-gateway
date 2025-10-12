import type { Page } from '@playwright/test'
import { WebRoute } from '@/lib/routes'
import { clearAllGlobalSettings, setupBothMfaMethodsForUser } from './db'
import { expect, test } from './fixtures'
import { login, satisfyStepUpModal } from './helpers'

const VIEWER_EMAIL = 'viewer@example.com'

async function navigateToSettings(page: Page) {
  await page.goto(WebRoute('settings'))
  await expect(page.getByRole('heading', { name: 'Settings' })).toBeVisible()
}

test.describe('Global Settings', () => {
  test.beforeEach(async ({ page }) => {
    // Clear all settings from database to ensure clean state
    await clearAllGlobalSettings()

    await login(page)
    await navigateToSettings(page)
  })

  test('displays settings with correct source indicators', async ({ page }) => {
    // Wait for settings to load - use heading for page title
    await expect(page.getByRole('heading', { name: 'Settings' })).toBeVisible()

    // Verify both cards are visible
    await expect(page.getByText('MFA Configuration').first()).toBeVisible()
    await expect(page.getByText('Allow Destructive Actions').first()).toBeVisible()

    // Check for environment variable source indicator
    // The E2E test has RGW_SETTING_MFA_TRUSTED_DEVICE_TTL_DAYS=45 set
    await expect(page.getByText('from env: RGW_SETTING_MFA_TRUSTED_DEVICE_TTL_DAYS')).toBeVisible()

    // Verify the value from env is displayed
    const ttlInput = page.getByLabel(/trusted device ttl/i)
    await expect(ttlInput).toHaveValue('45')

    // Check for default source indicators on other settings
    const defaultIndicators = page.getByText('(default)')
    const count = await defaultIndicators.count()
    expect(count).toBeGreaterThan(0)
  })

  test('can update a setting and see Save/Cancel buttons', async ({ page }) => {
    // Wait for settings to load
    await expect(page.getByText('Allow Destructive Actions').first()).toBeVisible()

    // Initially no Save/Cancel buttons should be visible in the Destructive Actions card
    const allowDestructiveCheckbox = page.getByLabel(/allow destructive actions/i)
    await expect(allowDestructiveCheckbox).toBeVisible()

    // Check the current state
    const isChecked = await allowDestructiveCheckbox.isChecked()

    // Toggle the checkbox
    await allowDestructiveCheckbox.click()

    // Save and Cancel buttons should appear
    const saveButtons = page.getByRole('button', { name: /^save$/i })
    const cancelButtons = page.getByRole('button', { name: /^cancel$/i })

    // Should have at least one Save button visible (from the card we changed)
    await expect(saveButtons.first()).toBeVisible()
    await expect(cancelButtons.first()).toBeVisible()

    // Click Cancel to revert changes
    await cancelButtons.first().click()

    // Checkbox should be back to original state
    if (isChecked) {
      await expect(allowDestructiveCheckbox).toBeChecked()
    } else {
      await expect(allowDestructiveCheckbox).not.toBeChecked()
    }

    // Save/Cancel buttons should disappear
    await expect(saveButtons.first()).not.toBeVisible({ timeout: 2000 })
  })

  test('can save a setting to database and clear it back to default', async ({ page }) => {
    // Wait for settings to load
    await expect(page.getByText('Allow Destructive Actions').first()).toBeVisible()

    const allowDestructiveCheckbox = page.getByLabel(/allow destructive actions/i)

    // Verify default state (unchecked)
    await expect(allowDestructiveCheckbox).not.toBeChecked()

    // Step 1: Check the checkbox
    await allowDestructiveCheckbox.click()
    await expect(allowDestructiveCheckbox).toBeChecked()

    // Step 2: Save (checked state)
    let updateResponsePromise = page.waitForResponse(
      (response) =>
        response.url().includes('/api/v1/admin/settings/allow_destructive_actions') &&
        response.request().method() === 'PUT'
    )

    let saveButton = page.getByRole('button', { name: /^save$/i }).first()
    await saveButton.click()
    await satisfyStepUpModal(page)
    await updateResponsePromise

    // Save/Cancel buttons should disappear
    await expect(saveButton).not.toBeVisible({ timeout: 2000 })

    // Clear button should now be visible (setting is in DB)
    const clearButton = page.getByRole('button', { name: /^clear$/i })
    await expect(clearButton.first()).toBeVisible()

    // Step 3: Uncheck the checkbox
    await allowDestructiveCheckbox.click()
    await expect(allowDestructiveCheckbox).not.toBeChecked()

    // Step 4: Save (unchecked state)
    updateResponsePromise = page.waitForResponse(
      (response) =>
        response.url().includes('/api/v1/admin/settings/allow_destructive_actions') &&
        response.request().method() === 'PUT'
    )

    saveButton = page.getByRole('button', { name: /^save$/i }).first()
    await saveButton.click()
    await satisfyStepUpModal(page)
    await updateResponsePromise

    // Save/Cancel buttons should disappear
    await expect(saveButton).not.toBeVisible({ timeout: 2000 })

    // Clear button should still be visible (setting is in DB with explicit false value)
    await expect(clearButton.first()).toBeVisible()

    // Step 5: Clear to revert to default
    const clearResponsePromise = page.waitForResponse(
      (response) =>
        response.url().includes('/api/v1/admin/settings/allow_destructive_actions') &&
        response.request().method() === 'DELETE'
    )
    await clearButton.first().click()
    await satisfyStepUpModal(page)
    await clearResponsePromise

    // Checkbox should be back to default (false for allow_destructive_actions)
    await expect(allowDestructiveCheckbox).not.toBeChecked()

    // Clear button should disappear (no longer in DB)
    await expect(clearButton.first()).not.toBeVisible({ timeout: 2000 })

    // Source indicator should show "default"
    const destructiveCard = page.locator('text=Allow Destructive Actions').locator('..')
    await expect(destructiveCard.getByText('(default)')).toBeVisible()
  })

  test('can update multiple MFA settings at once', async ({ page }) => {
    // Wait for settings to load
    await expect(page.getByText('MFA Configuration').first()).toBeVisible()

    // The TTL input should have value from env var (45)
    const ttlInput = page.getByLabel(/trusted device ttl/i)
    await expect(ttlInput).toHaveValue('45')

    // Change step-up window
    const stepUpInput = page.getByLabel(/step-up window/i)
    await stepUpInput.fill('15')

    // Save button should appear in MFA card
    const saveButton = page.getByRole('button', { name: /^save$/i }).first()
    await expect(saveButton).toBeVisible()

    // Wait for API responses (should update step-up window)
    const updateResponsePromise = page.waitForResponse(
      (response) =>
        response.url().includes('/api/v1/admin/settings/mfa_step_up_window_minutes') &&
        response.request().method() === 'PUT'
    )

    await saveButton.click()
    await satisfyStepUpModal(page)
    await updateResponsePromise

    // Reload page to verify change persisted
    await page.reload()
    await expect(page.getByText('MFA Configuration').first()).toBeVisible()

    // Step-up window should still be 15
    await expect(page.getByLabel(/step-up window/i)).toHaveValue('15')
  })

  test('cannot modify settings as non-admin user', async ({ page }) => {
    // Set up MFA for viewer user so they can access the settings page
    await setupBothMfaMethodsForUser(VIEWER_EMAIL)

    // Logout admin
    await page.getByRole('button', { name: /^logout$/i }).click()
    await page.waitForURL(/web\/login$/)

    // Login as viewer
    await page.goto(WebRoute('login'))
    const loginButton = page
      .getByTestId('login-cta')
      .or(page.getByRole('button', { name: /Continue with/i }))
      .or(page.getByRole('link', { name: /Continue with/i }))
    await expect(loginButton).toBeVisible({ timeout: 5000 })
    const navPromise = page.waitForURL(/oauth2\/v2\/auth|dev\/select-user/i)
    await loginButton.click()
    await navPromise

    // Select viewer user
    const viewerCard = page.locator('text=Viewer User').first()
    await expect(viewerCard).toBeVisible()
    await viewerCard.click()
    await page.waitForURL(/app/, { timeout: 15_000 })

    // Navigate to settings
    await navigateToSettings(page)

    // All inputs should be disabled for non-admin
    const checkboxes = page.getByRole('checkbox')
    const checkboxCount = await checkboxes.count()
    for (let i = 0; i < checkboxCount; i++) {
      await expect(checkboxes.nth(i)).toBeDisabled()
    }

    const numberInputs = page.getByRole('spinbutton')
    const numberInputCount = await numberInputs.count()
    for (let i = 0; i < numberInputCount; i++) {
      await expect(numberInputs.nth(i)).toBeDisabled()
    }

    // No Save/Cancel buttons should be visible (user can't make changes)
    await expect(page.getByRole('button', { name: /^save$/i })).not.toBeVisible()
    await expect(page.getByRole('button', { name: /^cancel$/i })).not.toBeVisible()

    // Clear button may be visible if settings are in DB, but it should be disabled
    const clearButtons = page.getByRole('button', { name: /^clear$/i })
    const clearButtonCount = await clearButtons.count()
    if (clearButtonCount > 0) {
      // All Clear buttons should be disabled for non-admin
      for (let i = 0; i < clearButtonCount; i++) {
        await expect(clearButtons.nth(i)).toBeDisabled()
      }
    }
  })
})
