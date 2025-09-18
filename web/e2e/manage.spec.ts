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

test("users: add, edit role, delete", async ({ page }) => {
  await login(page);

  // Navigate to Users
  await page.goto("/.gateway/web/users");
  await expect(page.getByRole("heading", { name: /Users/i })).toBeVisible();

  const email = `e2e-user-${Date.now()}@example.com`;

  // Add user
  await page.getByRole("button", { name: /Add User/i }).click();
  await page.getByLabel("Email").fill(email);
  await page.getByLabel("Name").fill("E2E User");
  // Role defaults to viewer; save
  await page.getByRole("button", { name: /Add User/i }).click();

  // Verify row appears
  const row = page.locator("tr", { hasText: email });
  await expect(row).toBeVisible();

  // Ensure "Added By" column exists and has a value for this row
  const headers = page.locator("table thead th");
  await expect(headers.getByText(/Added By/i)).toBeVisible();
  const headerTexts = await headers.allTextContents();
  const addedByIdx = headerTexts.findIndex((t: string) => /Added By/i.test(t));
  if (addedByIdx >= 0) {
    const addedByCell = row.locator("td").nth(addedByIdx);
    await expect(addedByCell).toHaveText(/.+/);
  }

  // Edit role to admin
  await row.getByRole("button", { name: /Edit User/i }).click();
  // Choose Administrator within the open dialog to avoid strict matches
  const dialog = page.getByRole("dialog");
  await dialog.getByText("Administrator").click();
  await page.getByRole("button", { name: /Update User/i }).click();
  // Role badge should show Administrator
  await expect(row.getByText("Administrator")).toBeVisible();

  // Delete user (confirm dialog)
  page.once("dialog", (d) => d.accept());
  await row.getByRole("button", { name: /Delete User/i }).click();
  await expect(row).toHaveCount(0);
});

test("users: add shows all fields and persists after refresh", async ({
  page,
}) => {
  await login(page);

  // Navigate to Users
  await page.goto("/.gateway/web/users");
  await expect(page.getByRole("heading", { name: /Users/i })).toBeVisible();

  const email = `e2e-persist-${Date.now()}@example.com`;

  // Add user
  await page.getByRole("button", { name: /Add User/i }).click();
  await page.getByLabel("Email").fill(email);
  await page.getByLabel("Name").fill("E2E Persist");
  await page.getByRole("button", { name: /Add User/i }).click();

  // Verify row appears with expected fields
  let row = page.locator("tr", { hasText: email });
  await expect(row).toBeVisible();

  // Refresh and ensure the row and fields persist, then validate columns
  await page.reload();
  row = page.locator("tr", { hasText: email });
  await expect(row).toBeVisible();
  // Determine column indices after reload
  const headers = page.locator("table thead th");
  const headerTexts = await headers.allTextContents();
  const createdIdx = headerTexts.findIndex((t: string) => /Created/i.test(t));
  const addedByIdx = headerTexts.findIndex((t: string) => /Added By/i.test(t));
  if (createdIdx >= 0) {
    const createdCell = row.locator("td").nth(createdIdx);
    const createdText = (await createdCell.innerText()).trim();
    expect(createdText).not.toBe("—");
    expect(createdText).not.toBe("-");
    expect(createdText.length).toBeGreaterThan(0);
  }
  if (addedByIdx >= 0) {
    const addedByCell = row.locator("td").nth(addedByIdx);
    await expect(addedByCell).toHaveText(/admin@/i);
  }
});

test("tokens: create, rename, delete", async ({ page }) => {
  await login(page);

  // Navigate to API Tokens
  await page.goto("/.gateway/web/api_tokens");
  await expect(
    page.getByRole("heading", { name: /API Tokens/i })
  ).toBeVisible();

  const name1 = `E2E Token ${Date.now()}`;

  // Create token
  await page.getByRole("button", { name: /Create Token/i }).click();
  await page.getByLabel("Token Name").fill(name1);
  await page.getByRole("button", { name: /Create Token/i }).click();
  // Close created token dialog
  await page.getByRole("button", { name: /Done/i }).click();

  // Verify row appears
  const row = page.locator("tr", { hasText: name1 });
  await expect(row).toBeVisible();

  // Rename token with explicit aria label
  await row.getByRole("button", { name: /Edit Token/i }).click();
  const name2 = `${name1} Renamed`;
  await page.getByLabel("Token Name").fill(name2);
  await page.getByRole("button", { name: /^Save$/ }).click();
  await expect(page.locator("tr", { hasText: name2 })).toBeVisible();

  // Delete token
  const row2 = page.locator("tr", { hasText: name2 });
  // Delete token using aria label
  await row2.getByRole("button", { name: /Delete Token/i }).click();
  // Confirm modal: type DELETE then confirm
  const confirmDialog = page.getByRole("dialog");
  await confirmDialog.getByLabel("Confirmation").fill("DELETE");
  await confirmDialog.getByRole("button", { name: /Delete Token/i }).click();
  await expect(row2).toHaveCount(0);
});

test("audit logs: view and filter", async ({ page }) => {
  await login(page);

  // Create a token to ensure we have a recent audit entry to filter
  await page.goto("/.gateway/web/api_tokens");
  await expect(
    page.getByRole("heading", { name: /API Tokens/i })
  ).toBeVisible();
  const tokenName = `E2E Audit Token ${Date.now()}`;
  await page.getByRole("button", { name: /Create Token/i }).click();
  await page.getByLabel("Token Name").fill(tokenName);
  await page.getByRole("button", { name: /Create Token/i }).click();
  await page.getByRole("button", { name: /Done/i }).click();

  // Navigate to Audit Logs
  await page.goto("/.gateway/web/audit_logs");
  await expect(
    page.getByRole("heading", { name: /Audit Logs/i })
  ).toBeVisible();

  // Ensure table rendered
  const table = page.getByRole("table");
  await expect(table).toBeVisible();

  // Filter by Action Type: Token Management
  await page.locator("#action-type").click();
  await page.getByRole("option", { name: /Token Management/i }).click();

  // Filter by Status: Success
  await page.locator("#status").click();
  await page.getByRole("option", { name: /Success/i }).click();

  // Search for the created token name
  await page.getByLabel("Search").fill(tokenName);

  // Verify at least one data row remains
  await expect(page.locator("table tbody tr").first()).toBeVisible();
});
