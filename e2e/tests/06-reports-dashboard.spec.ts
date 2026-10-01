import { expect, test, type APIRequestContext, type Page } from "@playwright/test";

const shots = process.env.SCREENSHOT_DIR ?? "test-results/screens";
const shot = (page: Page, name: string) => page.screenshot({ path: `${shots}/${name}.png`, fullPage: true });
const admin = { email: "admin@example.com", password: "correct horse battery" };
const headers = { "X-Viceroy-CSRF": "1" };

test.describe.configure({ mode: "serial" });

async function login(page: Page) {
  await page.goto("/login");
  await page.getByLabel("Email").fill(admin.email);
  await page.getByLabel("Password").fill(admin.password);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page.getByRole("heading", { name: "Dashboard", level: 1 })).toBeVisible();
}

const iso = (d: Date) => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
const daysAgo = (n: number) => {
  const d = new Date();
  d.setDate(d.getDate() - n);
  return iso(d);
};
const monthsAgo = (n: number, day: number) => {
  const d = new Date();
  return iso(new Date(d.getFullYear(), d.getMonth() - n, day));
};

/** Seeds a checking account with a few months of categorized history through the API. */
async function seed(req: APIRequestContext) {
  const acct = await (await req.post("/api/accounts", { headers, data: { name: "Reports Checking", type: "checking", balance: "5000" } })).json();
  const budget = await (await req.get("/api/budget")).json();
  const cat: Record<string, number> = {};
  for (const g of budget.groups) for (const l of g.lines) cat[l.name] = l.id;
  const add = async (date: string, amount: string, description: string, category: string) => {
    const res = await req.post("/api/transactions", { headers, data: { account_id: acct.id, date, amount, description } });
    expect(res.ok()).toBeTruthy();
    const t = await res.json();
    await req.patch(`/api/transactions/${t.id}`, { headers, data: { category_id: cat[category] } });
  };
  for (let m = 0; m < 4; m++) {
    await add(monthsAgo(m, 1), "3200", "Acme Payroll", "Paychecks");
    await add(monthsAgo(m, 2), "-1450", "Parkside Apartments", "Rent");
    await add(monthsAgo(m, 3), `-${120 + m * 15}`, "Whole Foods", "Groceries");
    await add(monthsAgo(m, 4), `-${40 + m * 5}`, "Bistro Nord", "Restaurants & Bars");
  }
  for (const back of [63, 32, 1]) await add(daysAgo(back), "-15.99", "Streamflix", "Subscriptions");
}

test("reports: cash flow, spending, income and net worth", async ({ page }) => {
  await login(page);
  await seed(page.request);
  await page.getByRole("link", { name: "Reports" }).first().click();
  await expect(page.getByRole("heading", { name: "Reports", level: 1 })).toBeVisible();

  await page.getByRole("tab", { name: "Cash flow" }).click();
  await expect(page.getByTestId("cashflow-stats")).toContainText("Savings rate");
  await expect(page.getByRole("img", { name: "Cash flow chart" })).toBeVisible();
  await expect(page.getByTestId("breakdown-table").first()).toContainText("Paychecks");
  await expect(page.getByTestId("breakdown-table").nth(1)).toContainText("Rent");
  await shot(page, "60-reports-cashflow");

  await page.getByRole("tab", { name: "Spending" }).click();
  await expect(page.getByRole("img", { name: "Spending over time chart" })).toBeVisible();
  await expect(page.getByTestId("breakdown-table")).toContainText("Groceries");
  await page.getByRole("button", { name: "Merchant" }).click();
  await expect(page.getByTestId("breakdown-table")).toContainText("Parkside Apartments");
  await page.getByRole("button", { name: "Breakdown" }).click();
  await expect(page.getByRole("img", { name: "Spending breakdown chart" })).toBeVisible();
  await shot(page, "61-reports-spending");

  await page.getByRole("button", { name: "Group", exact: true }).click();
  await expect(page.getByTestId("breakdown-table")).toContainText("Fixed");
  await page.getByRole("button", { name: "Quarterly" }).click();
  await page.getByRole("button", { name: "12M" }).click();
  await page.getByRole("button", { name: "Over time" }).click();
  await expect(page.getByRole("img", { name: "Spending over time chart" })).toBeVisible();

  await page.getByRole("tab", { name: "Income" }).click();
  await page.getByRole("button", { name: "Category" }).click();
  await expect(page.getByTestId("breakdown-table")).toContainText("Paychecks");

  await page.getByRole("tab", { name: "Net worth" }).click();
  await expect(page.getByTestId("networth-table")).toContainText("Cash");
  await page.getByRole("button", { name: "By type" }).click();
  await expect(page.getByRole("img", { name: "Net worth by account type chart" })).toBeVisible();
  await shot(page, "62-reports-networth");

  // The chosen tab is remembered.
  await page.reload();
  await expect(page.getByRole("tab", { name: "Net worth" })).toHaveAttribute("aria-selected", "true");
});

test("dashboard widgets and recurring", async ({ page }) => {
  await login(page);
  await expect(page.getByRole("img", { name: "Net worth" })).toBeVisible();
  await expect(page.getByRole("img", { name: "Spending this month vs last month chart" })).toBeVisible();
  await expect(page.getByTestId("dashboard-budget")).toContainText("Flexible");
  await expect(page.getByTestId("dashboard-recurring")).toContainText("Streamflix");
  await expect(page.getByRole("heading", { name: "Goals" })).toBeVisible();
  await expect(page.getByRole("heading", { name: "Investments" })).toBeVisible();
  await shot(page, "63-dashboard");

  await page.getByRole("button", { name: /View all/ }).click();
  const dlg = page.getByRole("dialog");
  const row = dlg.getByTestId("recurring-row").filter({ hasText: "Streamflix" });
  await expect(row).toContainText("Monthly");
  await row.getByRole("button", { name: "Dismiss" }).click();
  await expect(dlg.getByTestId("recurring-list").getByText("Streamflix")).toBeHidden();
  await dlg.getByRole("button", { name: /dismissed/ }).click();
  await expect(dlg.getByTestId("recurring-row").filter({ hasText: "Streamflix" })).toContainText("Restore");
  await page.keyboard.press("Escape");
  // The card may be empty now (nothing else due soon), so check the page, not the list.
  await expect(page.locator("main").getByText("Streamflix")).toHaveCount(0);
});

test("dashboard and reports on mobile", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await login(page);
  await expect(page.getByRole("img", { name: "Spending this month vs last month chart" })).toBeVisible();
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
  expect(overflow).toBeLessThanOrEqual(0);
  await shot(page, "64-dashboard-mobile");
  await page.goto("/reports");
  await page.getByRole("tab", { name: "Spending" }).click();
  await expect(page.getByTestId("breakdown-table")).toBeVisible();
  const overflow2 = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
  expect(overflow2).toBeLessThanOrEqual(0);
  await shot(page, "65-reports-mobile");
});
