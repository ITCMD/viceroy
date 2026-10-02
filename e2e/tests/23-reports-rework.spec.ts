import { expect, test, type Page } from "@playwright/test";

const shots = process.env.SCREENSHOT_DIR ?? "test-results/screens";
const shot = async (page: Page, name: string) => {
  await page.waitForTimeout(700); // let chart animations finish
  await page.screenshot({ path: `${shots}/${name}.png`, fullPage: true });
};
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

const monthName = (back: number) => {
  const d = new Date();
  return new Date(d.getFullYear(), d.getMonth() - back, 1).toLocaleDateString("en-US", { month: "long", year: "numeric" });
};

// Builds on the history 06-reports-dashboard seeded (Reports Checking: payroll, rent, groceries, dining).

test("cash flow: flow diagram, period arrows", async ({ page }) => {
  await login(page);
  await page.goto("/reports");
  await page.getByRole("tab", { name: "Cash flow" }).click();
  await page.getByRole("button", { name: "Flow" }).click();
  await page.getByRole("button", { name: "1M" }).click();
  const label = page.getByTestId("report-period-label");
  await expect(label).toHaveText(monthName(0));
  await page.getByRole("button", { name: "Previous period" }).click();
  await expect(label).toHaveText(monthName(1));
  await expect(page.getByRole("img", { name: "Cash flow diagram" })).toBeVisible();
  await expect(page.getByTestId("cashflow-sankey")).toContainText("Click a box");
  await shot(page, "230-cashflow-sankey");
  await page.getByRole("button", { name: "Previous period" }).click();
  await expect(label).toHaveText(monthName(2));
  await page.getByRole("button", { name: "Today" }).click();
  await expect(label).toHaveText(monthName(0));
  await expect(page.getByRole("button", { name: "Next period" })).toBeDisabled();
  await page.getByRole("button", { name: "3M" }).click();
  await expect(label).toContainText("–");
});

test("spending: income beside the bars, drill-down map to transactions", async ({ page }) => {
  await login(page);
  await page.goto("/reports");
  await page.getByRole("tab", { name: "Spending" }).click();
  await page.getByRole("button", { name: "6M" }).click();
  await page.getByRole("button", { name: "Category" }).click();
  await page.getByRole("button", { name: "Over time" }).click();
  await expect(page.getByRole("img", { name: "Spending over time chart" })).toBeVisible();
  await expect(page.locator("main ul").filter({ hasText: "Income" }).first()).toBeVisible();
  await shot(page, "231-spending-over-time");

  await page.getByRole("button", { name: "Breakdown" }).click();
  const map = page.getByTestId("treemap");
  await expect(map).toBeVisible();
  await shot(page, "232-spending-map-top");
  await map.getByRole("listitem", { name: /^Fixed,/ }).click();
  await expect(page.getByTestId("treemap-crumbs")).toContainText("Fixed");
  await map.getByRole("listitem", { name: /^Rent,/ }).click();
  await expect(page.getByTestId("treemap-crumbs")).toContainText("Rent");
  await shot(page, "232-spending-map-rent");
  await map.getByRole("listitem", { name: /^Parkside Apartments,/ }).click();
  await expect(page.getByRole("heading", { name: "Transactions", level: 1 })).toBeVisible();
  await expect(page.getByTestId("txn-link-filter")).toContainText("Parkside Apartments");
  await expect(page.getByTestId("txn-row").first()).toContainText("Parkside Apartments");
  const merchants = await page.getByTestId("txn-row").allTextContents();
  expect(merchants.every((m) => m.includes("Parkside Apartments"))).toBeTruthy();

  // Back to the map's top level via the crumbs.
  await page.goBack();
  await page.getByRole("button", { name: "Where it went" }).click();
  await expect(map.getByRole("listitem", { name: /^Flexible,/ })).toBeVisible();
});

test("transactions: filter by type, amount, category and dates", async ({ page }) => {
  await login(page);
  await page.goto("/transactions");
  await page.getByTestId("txn-filters-toggle").click();
  const panel = page.getByTestId("txn-filter-panel");
  await panel.getByRole("button", { name: "Income" }).click();
  await expect(page.getByTestId("txn-filter-chip")).toContainText("Income");
  await expect(page.getByTestId("txn-row").first()).toContainText("Acme Payroll");
  expect((await page.getByTestId("txn-row").allTextContents()).every((t) => !t.includes("Whole Foods"))).toBeTruthy();

  await panel.getByRole("button", { name: "Expenses" }).click();
  await panel.getByLabel("Min $").fill("1000");
  await expect(page.getByTestId("txn-filter-chip").filter({ hasText: "$1,000 or more" })).toBeVisible();
  // Every row left is money out of $1,000 or more (other specs add their own big ones).
  const amounts = async () =>
    (await page.getByTestId("txn-row").allTextContents()).map((t) => Number((t.match(/-\$([\d,]+\.\d\d)/)?.[1] ?? "0").replace(/,/g, "")));
  await expect.poll(async () => (await amounts()).every((a) => a >= 1000)).toBeTruthy();
  await expect(page.getByTestId("txn-row").filter({ hasText: "Parkside Apartments" }).first()).toBeVisible();
  await page.getByRole("button", { name: "Remove filter $1,000 or more" }).click();

  await panel.getByRole("button", { name: "Category" }).click();
  await page.getByPlaceholder(/Search/).last().fill("Groceries");
  await page.getByRole("option", { name: /Groceries/ }).first().click();
  await expect(page.getByTestId("txn-filter-chip").filter({ hasText: "Groceries" })).toBeVisible();
  await expect.poll(async () => (await page.getByTestId("txn-row").allTextContents()).every((t) => t.includes("Groceries"))).toBeTruthy();
  const all = await page.getByTestId("txn-row").count();
  await panel.getByRole("button", { name: "Last month" }).click();
  await expect.poll(() => page.getByTestId("txn-row").count()).toBeLessThan(all);
  await expect(page.getByTestId("txn-row").filter({ hasText: "-$135.00" })).toBeVisible(); // last month's Whole Foods
  await shot(page, "233-transaction-filters");
  await page.getByRole("button", { name: "Clear all", exact: true }).click();
  await expect(page.getByTestId("txn-filter-chip")).toHaveCount(0);
});

test("debt free future: debts, terms and payoff plans", async ({ page }) => {
  await login(page);
  await page.request.post("/api/accounts", { headers, data: { name: "Visa Rewards", type: "credit_card", balance: "4200" } });
  await page.request.post("/api/accounts", { headers, data: { name: "Car Loan", type: "loan", balance: "12000" } });
  await page.goto("/reports");
  await page.getByRole("tab", { name: "Debt Free Future" }).click();
  const table = page.getByTestId("debt-table");
  await expect(table).toContainText("Visa Rewards");
  await expect(table.getByTestId("debt-row").filter({ hasText: "Car Loan" })).toContainText("Not set");

  await page.getByRole("button", { name: "Edit Car Loan rate and minimum" }).click();
  await page.getByLabel("APR (%)").fill("6.9");
  await page.getByLabel("Minimum payment ($)").fill("310");
  await page.getByRole("button", { name: "Save" }).click();
  await expect(table.getByTestId("debt-row").filter({ hasText: "Car Loan" })).toContainText("6.9%");
  await expect(table.getByTestId("debt-row").filter({ hasText: "Car Loan" })).toContainText("$310.00");

  await page.getByLabel("Extra each month").fill("250");
  await expect(page.getByTestId("debt-insight")).toContainText("$250 extra a month saves");
  // Highest rate first: the 22% card before the 6.9% loan (other specs add their own debts).
  await expect(page.getByTestId("debt-order")).toContainText("Visa Rewards");
  const order = await page.getByTestId("debt-order").locator("li").allTextContents();
  expect(order.findIndex((t) => t.includes("Visa Rewards"))).toBeLessThan(order.findIndex((t) => t.includes("Car Loan")));
  await page.getByRole("button", { name: /Snowball/ }).click();
  await expect(page.getByTestId("debt-plans")).toContainText("Snowball");
  await shot(page, "234-debt-free-future");
  await page.getByTestId("debt-order").scrollIntoViewIfNeeded();
  await shot(page, "234-debt-plan");
});

test("discuss a report with the AI", async ({ page }) => {
  await login(page);
  await page.goto("/reports");
  await page.getByRole("tab", { name: "Cash flow" }).click();
  await page.getByTestId("discuss").click();
  await expect(page.getByRole("dialog")).toContainText("Discuss cash flow");
  await page.getByRole("button", { name: "Where is most of my money going?" }).click();
  await expect(page.getByTestId("chat-assistant").last()).toContainText("Looking at your Reports › Cash flow page");
  await shot(page, "235-discuss");
});

test("reports rework on mobile", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await login(page);
  for (const tab of ["Cash flow", "Spending", "Debt Free Future"]) {
    await page.goto("/reports");
    await page.getByRole("tab", { name: tab }).click();
    await page.waitForTimeout(300);
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
    expect(overflow, tab).toBeLessThanOrEqual(0);
  }
  await shot(page, "236-debt-mobile");
});
