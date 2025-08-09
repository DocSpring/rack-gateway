import { expect, test } from '@playwright/test'

// Mock authentication by setting a token
async function mockAuth(page, role: 'admin' | 'ops' | 'viewer') {
  // This would normally be done via the OAuth flow
  // For testing, we'll set a mock token directly
  await page.evaluate((role) => {
    const mockToken = 'mock-jwt-token'
    const mockUser = {
      email: `test-${role}@example.com`,
      name: `Test ${role}`,
      roles: [role],
    }

    // Set the cookie
    document.cookie = `gateway_token=${mockToken}; path=/; max-age=86400`

    // Mock the API response for getCurrentUser
    window.localStorage.setItem('mock_user', JSON.stringify(mockUser))
  }, role)
}

test.describe('User Management', () => {
  test.describe('Admin User', () => {
    test.beforeEach(async ({ page }) => {
      await mockAuth(page, 'admin')
      await page.goto('/users')
    })

    test('can view all users', async ({ page }) => {
      await expect(page.locator('h2')).toContainText('Users')
      await expect(page.locator('text=Add User')).toBeVisible()

      // Should see user list or empty state
      const userList = page.locator('ul').first()
      const emptyState = page.locator('text=No users configured')

      await expect(userList.or(emptyState)).toBeVisible()
    })

    test('can add a new user', async ({ page }) => {
      // Click Add User button
      await page.click('text=Add User')

      // Fill in the form
      await page.fill('input[type="email"]', 'newuser@example.com')
      await page.fill('input#name', 'New User')
      await page.check('text=deployer')

      // Submit the form
      await page.click('text=Add User')

      // Verify modal closes and user appears in list
      await expect(page.locator('text=newuser@example.com')).toBeVisible()
      await expect(page.locator('text=New User')).toBeVisible()
    })

    test('can edit an existing user', async ({ page }) => {
      // Assuming there's at least one user in the list
      // Click Edit on the first user
      const editButton = page.locator('text=Edit').first()
      await editButton.click()

      // Modify the name
      await page.fill('input#name', 'Updated Name')

      // Toggle a role
      await page.check('text=admin')

      // Save changes
      await page.click('text=Save Changes')

      // Verify modal closes
      await expect(page.locator('text=Edit User')).not.toBeVisible()
    })

    test('can delete a user', async ({ page }) => {
      // Mock confirmation dialog
      page.on('dialog', (dialog) => dialog.accept())

      // Click Delete on a user
      const deleteButton = page.locator('text=Delete').first()
      await deleteButton.click()

      // Verify user is removed
      // (Would need to check specific user email is gone)
    })

    test('validates email format', async ({ page }) => {
      await page.click('text=Add User')

      // Enter invalid email
      await page.fill('input[type="email"]', 'invalid-email')
      await page.fill('input#name', 'Test User')
      await page.check('text=viewer')

      await page.click('button:has-text("Add User")')

      // Should show validation error
      await expect(page.locator('text=Invalid email format')).toBeVisible()
    })

    test('requires at least one role', async ({ page }) => {
      await page.click('text=Add User')

      await page.fill('input[type="email"]', 'test@example.com')
      await page.fill('input#name', 'Test User')

      // Don't select any roles
      await page.click('button:has-text("Add User")')

      // Should show validation error
      await expect(page.locator('text=At least one role is required')).toBeVisible()
    })
  })

  test.describe('Ops User', () => {
    test.beforeEach(async ({ page }) => {
      await mockAuth(page, 'ops')
      await page.goto('/users')
    })

    test('can view users but not edit', async ({ page }) => {
      await expect(page.locator('h2')).toContainText('Users')
      await expect(page.locator('text=Read-only access')).toBeVisible()

      // Should NOT see Add User button
      await expect(page.locator('text=Add User')).not.toBeVisible()

      // Should NOT see Edit/Delete buttons
      await expect(page.locator('text=Edit')).not.toBeVisible()
      await expect(page.locator('text=Delete')).not.toBeVisible()
    })
  })

  test.describe('Viewer User', () => {
    test('cannot access user management', async ({ page }) => {
      await mockAuth(page, 'viewer')
      await page.goto('/users')

      // Should see access denied message
      await expect(page.locator('text=Access Restricted')).toBeVisible()
      await expect(page.locator('text=Your viewer role does not have access')).toBeVisible()
    })
  })
})

test.describe('Authentication Flow', () => {
  test('redirects to login when not authenticated', async ({ page }) => {
    await page.goto('/users')
    await expect(page).toHaveURL('/login')
  })

  test('shows Google sign-in button', async ({ page }) => {
    await page.goto('/login')
    await expect(page.locator('text=Sign in with Google')).toBeVisible()
  })

  test('handles OAuth callback', async ({ page }) => {
    // Simulate OAuth callback
    await page.goto('/auth/callback?code=test-code&state=test-state')

    // Should show loading state
    await expect(page.locator('text=Completing authentication')).toBeVisible()

    // In real test, would need to mock the backend response
  })
})
