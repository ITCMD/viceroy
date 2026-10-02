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
  await page.getByRole("link", { name: "Accounts" }).first().click();
  await expect(page.getByRole("heading", { name: "Accounts", level: 1 })).toBeVisible();
}

test("manual account and SimpleFIN connect", async ({ page, request }) => {
  await request.post(`${fake}/_control/scenario`, { data: { name: "initial" } });
  await login(page);
  await expect(page.getByText("No bank accounts yet")).toBeVisible();
  await expect(page.getByTestId("account-row").filter({ hasText: "Paper Cash" })).toBeVisible();

  await page.getByRole("button", { name: "Add account" }).first().click();
  await page.getByRole("button", { name: /Add a manual account/ }).click();
  await page.getByLabel("Account name").fill("Wallet");
  await page.getByLabel("Type").selectOption("cash");
  await page.getByLabel("Current balance").fill("40.25");
  await page.getByRole("dialog").getByRole("button", { name: "Add account" }).click();
  await expect(page.getByRole("dialog")).toBeHidden();
  await expect(page.getByTestId("account-row").filter({ hasText: "Wallet" })).toBeVisible();

  const token = await (await request.get(`${fake}/_control/token`)).text();
  await page.getByRole("button", { name: "Add account" }).first().click();
  await page.getByRole("button", { name: /Connect with SimpleFIN/ }).click();
  await page.getByLabel("Setup token").fill(token);
  await page.getByRole("button", { name: "Connect" }).click();
  // Connecting opens the account picker with everything on.
  const manage = page.getByTestId("manage-connection");
  await expect(manage.getByTestId("manage-row")).toHaveCount(6);
  await expect(manage.getByRole("switch", { checked: false })).toHaveCount(1); // auto-add starts off
  await shot(page, "10a-accounts-manage");
  await page.getByRole("dialog").getByRole("button", { name: "Close" }).first().click();
  await expect(page.getByRole("dialog")).toBeHidden();
  await expect(page.getByTestId("account-row")).toHaveCount(8);
  await expect(page.getByText("360 Checking (1111)")).toBeVisible();
  await expect(page.getByText("Online Savings (4444)")).toBeVisible();
  await shot(page, "10-accounts-networth");

  await page.getByRole("tab", { name: "Credit cards" }).click();
  await expect(page.getByTestId("account-row")).toHaveCount(3);
  await shot(page, "11-accounts-credit");
});

test("relink keeps history, review links duplicates", async ({ page, request }) => {
  await login(page);
  await request.post(`${fake}/_control/scenario`, { data: { name: "relinked" } });
  await page.getByRole("button", { name: "Sync now" }).click();
  const banner = page.getByRole("button", { name: /1 account needs review/ });
  await expect(banner).toBeVisible();
  // A card shared for the first time waits to be added.
  await expect(page.getByTestId("account-row").filter({ hasText: "Venture Card (5555)" })).toHaveCount(0);
  await expect(page.getByTestId("account-row").filter({ hasText: "360 Performance Savings" }).getByText("Disconnected")).toBeVisible();
  await expect(page.getByTestId("account-row").filter({ hasText: "360 Checking (1111)" }).getByText("Disconnected")).toHaveCount(0);
  await shot(page, "12-accounts-review-banner");

  await banner.click();
  await expect(page.getByTestId("review-row")).toHaveCount(1);
  await shot(page, "13-accounts-review-dialog");
  await page.getByRole("button", { name: "Link to existing" }).click();
  await expect(page.getByText("All accounts are reviewed.")).toBeVisible();
  await page.getByRole("button", { name: "Close" }).first().click();
  await expect(banner).toBeHidden();

  // Add the new card from the banner; its history comes with it.
  await page.getByRole("button", { name: /1 new account on SimpleFIN: Venture Card/ }).click();
  const manage = page.getByTestId("manage-connection");
  await expect(manage.getByTestId("manage-row").filter({ hasText: "Venture Card" }).getByText("New")).toBeVisible();
  await shot(page, "13a-accounts-new-offered");
  await manage.getByRole("switch", { name: /Venture Card/ }).click();
  await page.getByRole("dialog").getByRole("button", { name: "Save" }).click();
  await expect(page.getByRole("dialog")).toBeHidden();
  await expect(page.getByTestId("account-row").filter({ hasText: "Venture Card (5555)" })).toBeVisible();
  await expect(page.getByRole("button", { name: /new account on SimpleFIN/ })).toHaveCount(0);
});

test("account sheet renames and hides", async ({ page }) => {
  await login(page);
  await page.getByTestId("account-row").filter({ hasText: "Venture Card (5555)" }).click();
  const sheet = page.getByRole("dialog");
  await sheet.getByLabel("Name").fill("Travel card");
  await sheet.getByRole("button", { name: "Save changes" }).click();
  await expect(sheet.getByRole("heading", { name: "Travel card" })).toBeVisible();
  await shot(page, "14-account-sheet");
  // Flip a balance the bank reports backwards, and back.
  await expect(sheet.getByText("-$310.00").first()).toBeVisible();
  await sheet.getByRole("switch", { name: /Flip the bank's balance sign/ }).click();
  await expect(sheet.getByText("$310.00").first()).toBeVisible();
  await expect(sheet.getByText("-$310.00")).toHaveCount(0);
  await sheet.getByRole("switch", { name: /Flip the bank's balance sign/ }).click();
  await expect(sheet.getByText("-$310.00").first()).toBeVisible();
  // The color's hex code can be typed.
  await sheet.getByLabel("Hex color").fill("#123abc");
  await expect(sheet.getByLabel("Account color")).toHaveValue("#123abc");
  await sheet.getByRole("switch", { name: "Hide from lists" }).click();
  await page.keyboard.press("Escape");
  await expect(page.getByTestId("account-row").filter({ hasText: "Travel card" })).toHaveCount(0);
  await page.getByRole("button", { name: /Show 1 hidden/ }).click();
  await expect(page.getByTestId("account-row").filter({ hasText: "Travel card" })).toBeVisible();
});

test("mobile accounts layout", async ({ browser }) => {
  const ctx = await browser.newContext({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true });
  const page = await ctx.newPage();
  await page.goto("/login");
  await page.getByLabel("Email").fill(admin.email);
  await page.getByLabel("Password").fill(admin.password);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page.getByRole("heading", { name: "Dashboard", level: 1 })).toBeVisible();
  await page.goto("/accounts");
  await expect(page.getByText("Net worth").first()).toBeVisible();
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth);
  expect(overflow).toBe(false);
  await shot(page, "15-accounts-mobile");
  await ctx.close();
});
