import { expect, test } from "./fixtures";

async function login(page: any) {
  await page.goto("/.gateway/web/login");
  const btn = page
    .getByTestId("login-cta")
    .or(page.getByRole("button", { name: /Continue with/i }))
    .or(page.getByRole("link", { name: /Continue with/i }));
  await expect(btn).toBeVisible({ timeout: 5000 });
  await Promise.all([
    page.waitForNavigation({ url: /oauth2\/v2\/auth|dev\/select-user/i }),
    btn.click(),
  ]);

  // Mock OAuth user selection if shown
  const userCard = page.locator("text=Admin User");
  if (
    await userCard
      .first()
      .isVisible()
      .catch(() => false)
  ) {
    await userCard.first().click();
  }

  // Wait for session readiness
  await page.waitForResponse(
    (r: any) => r.url().includes("/.gateway/api/me") && r.status() === 200,
    { timeout: 20000 }
  );
}

test("users: edit name and email", async ({ page }) => {
  await login(page);

  // Navigate to Users
  await page.goto("/.gateway/web/users");
  await expect(page.getByRole("heading", { name: /Users/i })).toBeVisible();

  const email1 = `e2e-edit-${Date.now()}@example.com`;
  const name1 = "E2E Edit User";

  // Add user
  await page.getByRole("button", { name: /Add User/i }).click();
  await page.getByLabel("Email").fill(email1);
  await page.getByLabel("Name").fill(name1);
  await page.getByRole("button", { name: /Add User/i }).click();

  // Verify initial row appears
  let row = page.locator("tr", { hasText: email1 });
  await expect(row).toBeVisible();
  await expect(row).toContainText(name1);

  // Open edit dialog
  await row.getByRole("button", { name: /Edit User/i }).click();

  // Change name and email
  const email2 = `e2e-edit-${Date.now()}-updated@example.com`;
  const name2 = "E2E Edited User";
  const dialog = page.getByRole("dialog");
  await dialog.getByLabel("Email").fill(email2);
  await dialog.getByLabel("Name").fill(name2);

  // Save
  await dialog.getByRole("button", { name: /Update User/i }).click();

  // Verify row updated to new email and name
  row = page.locator("tr", { hasText: email2 });
  await expect(row).toBeVisible();
  await expect(row).toContainText(name2);

  // Refresh and re-verify persistence
  await page.reload();
  row = page.locator("tr", { hasText: email2 });
  await expect(row).toBeVisible();
  await expect(row).toContainText(name2);

  // Cleanup: delete the user
  page.once("dialog", (d) => d.accept());
  await row.getByRole("button", { name: /Delete User/i }).click();
  await expect(row).toHaveCount(0);
});
