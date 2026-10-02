import { expect, test, type Page } from "@playwright/test";

const shots = process.env.SCREENSHOT_DIR ?? "test-results/screens";
const shot = (page: Page, name: string) => page.screenshot({ path: `${shots}/${name}.png`, fullPage: true });
const fake = "http://127.0.0.1:28430";
const admin = { email: "admin@example.com", password: "correct horse battery" };

test.describe.configure({ mode: "serial" });

async function login(page: Page) {
  await page.goto("/login");
  await page.getByLabel("Email").fill(admin.email);
  await page.getByLabel("Password").fill(admin.password);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page.getByRole("heading", { name: "Dashboard", level: 1 })).toBeVisible();
}

async function openTransactions(page: Page) {
  await page.getByRole("link", { name: "Transactions" }).first().click();
  await expect(page.getByRole("heading", { name: "Transactions", level: 1 })).toBeVisible();
}

function isoDaysAgo(n: number) {
  const d = new Date();
  d.setDate(d.getDate() - n);
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
}

async function pickCategory(page: Page, scope: ReturnType<Page["getByRole"]>, label: string, name: string) {
  await scope.getByLabel(label, { exact: true }).click();
  await page.getByLabel("Search categories").fill(name.slice(0, 5));
  await page.getByRole("option", { name }).click();
}

test("list, detail and recategorize", async ({ page }) => {
  await login(page);
  await openTransactions(page);
  const tj = page.getByTestId("txn-row").filter({ hasText: "Trader Joe's" });
  await expect(tj).toBeVisible();
  await expect(page.getByTestId("txn-row").filter({ hasText: "Blue Bottle Coffee" })).toBeVisible();
  await shot(page, "20-transactions");

  await tj.click();
  const sheet = page.getByRole("dialog");
  await expect(sheet.getByTestId("original-statement")).toHaveText("TRADER JOE'S #552");
  await pickCategory(page, sheet, "Category", "Groceries");
  await expect(tj).toContainText("Groceries");
  await sheet.getByLabel("Notes").fill("weekly shop");
  await sheet.getByLabel("Tags").fill("food");
  await sheet.getByLabel("Tags").press("Enter");
  await expect(sheet.getByText("food")).toBeVisible();
  await shot(page, "21-transaction-sheet");
  await page.keyboard.press("Escape");

  await page.getByRole("button", { name: "Needs review", exact: true }).click();
  await expect(tj).toHaveCount(0);
  await page.getByRole("button", { name: "All", exact: true }).click();
  await page.getByPlaceholder(/Search merchants/).fill("weekly");
  await expect(page.getByTestId("txn-row")).toHaveCount(1);
});

test("pending entry warns on duplicates and links when it posts", async ({ page, request }) => {
  await login(page);
  await openTransactions(page);

  // Blue Bottle -12.50 posted 3 days ago: a pending entry for it warns.
  await page.getByRole("button", { name: /Add/ }).first().click();
  let dlg = page.getByRole("dialog");
  await dlg.getByLabel("Account").selectOption({ label: "360 Checking (1111)" });
  await dlg.getByLabel("Merchant").fill("Blue Bottle");
  await dlg.getByLabel("Amount").fill("12.50");
  await dlg.getByLabel("Date").fill(isoDaysAgo(3));
  await dlg.getByRole("button", { name: "Add pending entry" }).click();
  await expect(dlg.getByTestId("duplicate-warning")).toBeVisible();
  await shot(page, "22-pending-duplicate-warning");
  await dlg.getByRole("button", { name: "Close" }).click();

  // Chipotle hasn't posted yet.
  await page.getByRole("button", { name: /Add/ }).first().click();
  dlg = page.getByRole("dialog");
  await dlg.getByLabel("Account").selectOption({ label: "360 Checking (1111)" });
  await dlg.getByLabel("Merchant").fill("Chipotle");
  await dlg.getByLabel("Amount").fill("18.75");
  await pickCategory(page, dlg, "Category", "Restaurants & Bars");
  await dlg.getByLabel("Notes").fill("lunch with Sam");
  await dlg.getByRole("button", { name: "Add pending entry" }).click();
  const sheet = page.getByRole("dialog");
  await expect(sheet.getByRole("heading", { name: "Link to posted transaction" })).toBeVisible();
  await page.keyboard.press("Escape");
  const chipotle = page.getByTestId("txn-row").filter({ hasText: "Chipotle" });
  await expect(chipotle).toHaveCount(1);
  await expect(chipotle).toContainText("Pending entry");

  // The bank posts it; syncing links the two.
  await request.post(`${fake}/_control/scenario`, { data: { name: "relinked+posted" } });
  await page.getByRole("link", { name: "Accounts" }).first().click();
  await page.getByRole("button", { name: "Sync now" }).click();
  await expect(page.getByRole("button", { name: "Sync now" })).toBeEnabled();
  await openTransactions(page);
  await expect(chipotle).toHaveCount(1);
  await expect(chipotle).not.toContainText("Pending entry");
  await expect(chipotle.getByLabel("Linked to a pending entry")).toBeVisible();
  await expect(chipotle).toContainText("Restaurants & Bars");

  await chipotle.click();
  await expect(sheet.getByTestId("original-statement")).toHaveText("CHIPOTLE 2231 AUSTIN TX");
  await expect(sheet.getByTestId("linked-list")).toBeVisible();
  await expect(sheet.getByLabel("Notes")).toHaveValue("lunch with Sam");
  await shot(page, "23-linked-transaction");
  await sheet.getByRole("button", { name: "Unlink" }).click();
  await expect(sheet.getByTestId("linked-list")).toHaveCount(0);
  await page.keyboard.press("Escape");
  await expect(chipotle).toHaveCount(2);
});

test("rules page creates and applies a rule", async ({ page }) => {
  await login(page);
  await page.getByRole("link", { name: "Settings" }).first().click();
  await expect(page.getByText("No rules yet")).toBeVisible();
  await page.getByRole("button", { name: "Add rule" }).click();
  const dlg = page.getByRole("dialog");
  await dlg.getByLabel("Text").fill("uber");
  await pickCategory(page, dlg, "Set category", "Taxi & Ride Shares");
  await dlg.getByRole("button", { name: "Save rule" }).click();
  await expect(dlg.getByTestId("rule-apply")).toContainText("Apply it to 1 earlier transaction too?");
  await dlg.getByRole("button", { name: "Apply to earlier transactions" }).click();
  await expect(dlg.getByTestId("rule-applied")).toContainText("applied to 1 earlier transaction");
  await dlg.getByRole("button", { name: "Done" }).click();
  await expect(page.getByTestId("rule-row")).toContainText("Taxi & Ride Shares");
  await shot(page, "24-rules");

  await openTransactions(page);
  await expect(page.getByTestId("txn-row").filter({ hasText: "Uber" })).toContainText("Taxi & Ride Shares");
});

test("paper cash: standalone entries default to it, and it can be turned off", async ({ page }) => {
  await login(page);
  await openTransactions(page);
  await page.getByRole("button", { name: /Add/ }).first().click();
  const dlg = page.getByRole("dialog");
  await dlg.getByRole("button", { name: "Standalone" }).click();
  await expect(dlg.getByLabel("Account")).toHaveValue(await dlg.getByLabel("Account").locator("option", { hasText: "Paper Cash" }).getAttribute("value") ?? "");
  await dlg.getByLabel("Merchant").fill("Farmers Market");
  await dlg.getByLabel("Amount").fill("9");
  await dlg.getByRole("button", { name: "Add transaction" }).click();
  await expect(page.getByRole("dialog").getByTestId("original-statement")).toHaveText("Farmers Market");
  await page.keyboard.press("Escape");
  await expect(page.getByRole("dialog")).toBeHidden();
  await expect(page.getByTestId("txn-row").filter({ hasText: "Farmers Market" })).toContainText("Paper Cash");
  await page.getByRole("link", { name: "Accounts" }).first().click();
  await expect(page.getByTestId("account-row").filter({ hasText: "Paper Cash" })).toContainText("-$9.00");
  await openTransactions(page);

  await page.getByRole("link", { name: "Settings" }).first().click();
  const toggle = page.getByRole("switch", { name: "Paper Cash account" });
  await expect(toggle).toBeChecked();
  await toggle.click();
  await expect(toggle).not.toBeChecked();
  await shot(page, "27-settings-paper-cash");
  await page.getByRole("link", { name: "Accounts" }).first().click();
  await expect(page.getByTestId("account-row").filter({ hasText: "Paper Cash" })).toHaveCount(0);
  await openTransactions(page);
  await expect(page.getByTestId("txn-row").filter({ hasText: "Farmers Market" })).toBeVisible(); // history kept

  await page.getByRole("link", { name: "Settings" }).first().click();
  await toggle.click();
  await expect(toggle).toBeChecked();
});

test("mobile transactions layout", async ({ browser }) => {
  const ctx = await browser.newContext({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true });
  const page = await ctx.newPage();
  await login(page);
  await page.goto("/transactions");
  await expect(page.getByTestId("txn-row").first()).toBeVisible();
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth);
  expect(overflow).toBe(false);
  await shot(page, "25-transactions-mobile");
  await page.getByTestId("txn-row").first().click();
  await shot(page, "26-transaction-sheet-mobile");
  await ctx.close();
});
