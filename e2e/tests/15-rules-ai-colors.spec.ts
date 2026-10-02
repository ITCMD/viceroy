import { expect, test, type Page } from "@playwright/test";

const shots = process.env.SCREENSHOT_DIR ?? "test-results/screens";
const admin = { email: "admin@example.com", password: "correct horse battery" };
const headers = { "X-Viceroy-CSRF": "1" };
const png = Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==", "base64");

async function login(page: Page) {
  await page.goto("/login");
  await page.getByLabel("Email").fill(admin.email);
  await page.getByLabel("Password").fill(admin.password);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page.getByRole("heading", { name: "Dashboard", level: 1 })).toBeVisible();
}

async function addManualAccount(page: Page, name: string) {
  const res = await page.request.post("/api/accounts", { headers, data: { name, type: "checking", balance: "100" } });
  expect(res.ok()).toBeTruthy();
  return (await res.json()).id as number;
}

async function addTxn(page: Page, account: number, date: string, amount: string, description: string, pending = false) {
  const res = await page.request.post("/api/transactions", { headers, data: { account_id: account, date, amount, description, pending, force: true } });
  expect(res.ok()).toBeTruthy();
  return (await res.json()).id as number;
}

test("new accounts get their bank's color; color and logo can be changed", async ({ page }) => {
  await login(page);
  await addManualAccount(page, "DCU Checking");
  await page.goto("/accounts");
  const row = page.getByTestId("account-row").filter({ hasText: "DCU Checking" });
  // The light AI model picks DCU green in the background.
  await expect(row.getByTestId("account-avatar")).toHaveAttribute("data-color", "#00703c", { timeout: 15_000 });

  await row.click();
  const sheet = page.getByRole("dialog");
  const look = sheet.getByTestId("account-appearance");
  await expect(look).toContainText("Picked by AI");
  await look.getByLabel("Account color").fill("#ffffff");
  await look.getByRole("button", { name: "Save color" }).click();
  await expect(look).toContainText("Your color.");
  await look.getByTestId("logo-file").setInputFiles({ name: "dcu.png", mimeType: "image/png", buffer: png });
  await expect(look.getByTestId("account-logo")).toBeVisible();
  await page.screenshot({ path: `${shots}/15-account-look.png` });
  await page.keyboard.press("Escape");
  await expect(row.getByTestId("account-logo")).toBeVisible();
});

test("an edit offers a rule that applies to earlier and future transactions", async ({ page }) => {
  await login(page);
  const acct = await addManualAccount(page, "Rules Checking");
  await addTxn(page, acct, "2026-08-02", "-12.00", "SQ *PAPERCRAFT STUDIO 22");
  await addTxn(page, acct, "2026-08-16", "-9.00", "PAPERCRAFT STUDIO 22");
  const src = await addTxn(page, acct, "2026-09-02", "-15.00", "PAPERCRAFT STUDIO 22");

  await page.goto("/transactions");
  await page.getByPlaceholder("Search merchants, statements, notes").fill("papercraft");
  await expect(page.getByTestId("txn-row")).toHaveCount(3);
  await page.getByTestId("txn-row").first().click();
  const sheet = page.getByRole("dialog");
  await expect(sheet.getByTestId("rule-suggestion")).toHaveCount(0);
  await sheet.getByLabel("Category", { exact: true }).click();
  await page.getByLabel("Search categories").fill("Enter");
  await page.getByRole("option", { name: "Entertainment & Recreation" }).click();
  await sheet.getByLabel("Tags").fill("craft");
  await sheet.getByLabel("Tags").press("Enter");
  await expect(sheet.getByTestId("rule-suggestion")).toBeVisible();
  await sheet.getByRole("button", { name: "Create rule" }).click();

  const dialog = page.getByRole("dialog", { name: "Create rule" });
  await expect(dialog.getByTestId("rule-cond-text").getByLabel("Text")).toHaveValue("Papercraft Studio");
  await expect(dialog.getByTestId("rule-cond-account").getByRole("checkbox")).toBeChecked();
  await expect(dialog.getByTestId("rule-cond-amount").getByRole("checkbox")).not.toBeChecked();
  await expect(dialog.getByText("craft", { exact: true })).toBeVisible();
  // Narrow it to the first half of the month.
  await dialog.getByTestId("rule-cond-days").getByRole("checkbox").check();
  await dialog.getByLabel("To day").selectOption("15");
  await page.screenshot({ path: `${shots}/15-rule-from-edit.png` });
  await dialog.getByRole("button", { name: "Save rule" }).click();

  const saved = page.getByRole("dialog", { name: "Rule saved" });
  await expect(saved.getByTestId("rule-apply")).toContainText("Apply it to 1 earlier transaction too?");
  await saved.getByRole("button", { name: "Apply to earlier transactions" }).click();
  await expect(saved.getByTestId("rule-applied")).toContainText("applied to 1 earlier transaction");
  await saved.getByRole("button", { name: "Done" }).click();

  // The 2nd matched (day 2); the 16th didn't.
  const rows = page.getByTestId("txn-row");
  await expect(rows.filter({ hasText: "-$12.00" })).toContainText("Entertainment");
  await expect(rows.filter({ hasText: "-$9.00" })).not.toContainText("Entertainment");

  // A new pending entry follows the rule.
  const pend = await addTxn(page, acct, "2026-10-03", "-20.00", "PAPERCRAFT STUDIO 22", true);
  const detail = await (await page.request.get(`/api/transactions/${pend}`)).json();
  expect(detail.transaction.category_name).toContain("Entertainment");
  expect(detail.transaction.tags.map((t: { name: string }) => t.name)).toEqual(["craft"]);
  expect(src).toBeGreaterThan(0);
});

test("categorize uncategorized transactions with AI", async ({ page }) => {
  await login(page);
  const acct = await addManualAccount(page, "AI Checking");
  const today = new Date().toISOString().slice(0, 10);
  await addTxn(page, acct, today, "-4.75", "BLUE BOTTLE COFFEE 4411");
  await addTxn(page, acct, today, "-31.00", "OBSCURE VENDOR 8812");

  await page.goto("/transactions");
  await page.getByRole("button", { name: "More transaction actions" }).click();
  await page.getByRole("menuitem", { name: "Categorize with AI…" }).click();
  const dialog = page.getByRole("dialog", { name: "Categorize with AI" });
  await expect(dialog.getByLabel("Uncategorized transactions from the last")).toHaveValue("31");
  await dialog.getByRole("button", { name: "Categorize" }).click();
  await expect(dialog.getByTestId("ai-categorize-result")).toContainText("Categorized");
  await expect(dialog.getByTestId("ai-categorize-result")).toContainText("still uncategorized");
  await dialog.getByRole("button", { name: "Done" }).click();
  await page.getByPlaceholder("Search merchants, statements, notes").fill("blue bottle coffee");
  await expect(page.getByTestId("txn-row").first()).toContainText("Coffee Shops");

  // The next one from that merchant follows the AI's pick without asking again.
  const next = await addTxn(page, acct, today, "-5.25", "BLUE BOTTLE COFFEE 4411");
  const d = await (await page.request.get(`/api/transactions/${next}`)).json();
  expect(d.transaction.category_name).toBe("Coffee Shops");
  expect(d.transaction.category_source).toBe("history");
});
