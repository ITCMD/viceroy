import { expect, test, type Page } from "@playwright/test";

const shots = process.env.SCREENSHOT_DIR ?? "test-results/screens";
const shot = (page: Page, name: string) => page.screenshot({ path: `${shots}/${name}.png`, fullPage: true });
const admin = { email: "admin@example.com", password: "correct horse battery" };

test.describe.configure({ mode: "serial" });

async function login(page: Page) {
  await page.goto("/login");
  await page.getByLabel("Email").fill(admin.email);
  await page.getByLabel("Password").fill(admin.password);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page.getByRole("heading", { name: "Dashboard", level: 1 })).toBeVisible();
}

async function openBudget(page: Page) {
  await page.getByRole("link", { name: "Budget", exact: true }).first().click();
  await expect(page.getByRole("heading", { name: "Budget", level: 1 })).toBeVisible();
}

const line = (page: Page, name: string) => page.getByTestId("budget-line").filter({ hasText: name });

test("edit a category budget with history and timing", async ({ page }) => {
  await login(page);
  await openBudget(page);
  const month = new Date().toLocaleDateString("en-US", { month: "long", year: "numeric" });
  await expect(page.getByTestId("budget-period")).toHaveText(month);
  for (const kind of ["income", "fixed", "flexible", "non_monthly", "goals"]) {
    await expect(page.getByTestId(`budget-group-${kind}`)).toBeVisible();
  }

  await line(page, "Groceries").click();
  const dlg = page.getByRole("dialog");
  await expect(dlg.getByRole("heading", { name: "Groceries" })).toBeVisible();
  await expect(dlg.getByRole("img", { name: "Spent chart" })).toBeVisible();
  await dlg.getByTestId("budget-amount").fill("600");
  await dlg.getByRole("button", { name: "When is this spent?" }).click();
  await dlg.getByRole("button", { name: "Every few weeks" }).click();
  await expect(dlg.getByLabel("Starting from")).toBeVisible();
  await dlg.getByRole("switch", { name: /Apply to all future months/ }).click();
  await shot(page, "50-budget-edit");
  await dlg.getByRole("button", { name: "Save" }).click();
  await expect(dlg).toBeHidden();
  await expect(line(page, "Groceries")).toContainText("$600.00");
  await expect(line(page, "Groceries")).toContainText("Every 2 weeks");

  // Forward: next month keeps $600; rent set for this month only does not carry.
  await line(page, "Rent").click();
  await dlg.getByTestId("budget-amount").fill("1500");
  await dlg.getByRole("button", { name: "When is this spent?" }).click();
  await dlg.getByRole("button", { name: "On a day" }).click();
  await dlg.getByLabel("Day of the month").fill("1");
  await dlg.getByRole("button", { name: "Save" }).click();
  await expect(line(page, "Rent")).toContainText("$1,500.00");
  await expect(line(page, "Rent")).toContainText("On day 1");
  await shot(page, "51-budget-month");

  await page.getByRole("button", { name: "Next period" }).click();
  await expect(page.getByTestId("budget-period")).not.toHaveText(month);
  await expect(line(page, "Groceries")).toContainText("$600.00");
  await expect(line(page, "Rent")).not.toContainText("$1,500.00");
  await page.getByRole("button", { name: "Today" }).click();
  await expect(page.getByTestId("budget-period")).toHaveText(month);
});

test("week and paycheck views", async ({ page }) => {
  await login(page);
  await openBudget(page);
  await page.getByRole("button", { name: "Week", exact: true }).click();
  await expect(page.getByTestId("budget-period")).toHaveText(/ – /);
  await expect(page.getByText(/this week's share of the monthly budget/)).toBeVisible();
  // Rent is due on day 1, so only the week containing the 1st gets it.
  const rent = line(page, "Rent");
  await expect(rent).toBeVisible();
  const week = await page.getByTestId("budget-period").textContent();
  await page.getByRole("button", { name: "Next period" }).click();
  await expect(page.getByTestId("budget-period")).not.toHaveText(week ?? "");
  await shot(page, "52-budget-week");

  // Pay schedule from settings drives the paycheck view.
  await page.getByRole("link", { name: "Settings" }).first().click();
  await page.getByLabel("Paid").selectOption("biweekly");
  await page.getByLabel("A recent payday").fill("2026-01-02");
  await page.getByRole("button", { name: "Save" }).first().click();
  await expect(page.getByRole("button", { name: "Save" })).toHaveCount(0);
  await openBudget(page);
  await page.getByRole("button", { name: "Paycheck", exact: true }).click();
  await expect(page.getByText(/this paycheck's share/)).toBeVisible();
  // Biweekly from Jan 2: the current paycheck starts on the latest payday on or before today.
  const today = new Date();
  today.setHours(0, 0, 0, 0);
  const start = new Date(2026, 0, 2);
  start.setDate(start.getDate() + Math.floor(Math.round((today.getTime() - start.getTime()) / 86400000) / 14) * 14);
  const end = new Date(start);
  end.setDate(end.getDate() + 13);
  const fmt = (d: Date) => d.toLocaleDateString("en-US", { month: "short", day: "numeric", ...(d.getFullYear() !== today.getFullYear() && { year: "numeric" }) });
  if (start.getFullYear() === today.getFullYear() && end.getFullYear() === today.getFullYear()) {
    await expect(page.getByTestId("budget-period")).toHaveText(`${fmt(start)} – ${fmt(end)}`);
  }
  await page.reload();
  await expect(page.getByRole("button", { name: "Paycheck", exact: true })).toHaveAttribute("aria-pressed", "true");
  await page.getByRole("button", { name: "Month", exact: true }).click();
});

test("goals: create, budget a contribution, assign a transaction", async ({ page }) => {
  await login(page);
  await page.getByRole("link", { name: "Goals" }).first().click();
  await expect(page.getByText("No goals yet")).toBeVisible();
  await page.getByRole("button", { name: "Add goal" }).click();
  const dlg = page.getByRole("dialog");
  await dlg.getByLabel("Name").fill("Vacation");
  await dlg.getByLabel("Target amount").fill("3000");
  await dlg.getByLabel("Already saved").fill("100");
  await dlg.getByRole("button", { name: "Save" }).click();
  const card = page.getByTestId("goal-card").filter({ hasText: "Vacation" });
  await expect(card).toContainText("$100.00");
  await expect(card).toContainText("3% there");

  await openBudget(page);
  const goalLine = page.getByTestId("budget-group-goals").getByTestId("budget-line").filter({ hasText: "Vacation" });
  await goalLine.click();
  await expect(dlg.getByText(/Contribution for/)).toBeVisible();
  await dlg.getByTestId("budget-amount").fill("200");
  await dlg.getByRole("button", { name: "Save" }).click();
  await expect(goalLine).toContainText("$200.00");

  // Assign a transaction to the goal from the transaction sheet.
  await page.getByRole("link", { name: "Transactions" }).first().click();
  await page.getByTestId("txn-row").first().click();
  const sheet = page.getByRole("dialog");
  await sheet.getByLabel("Contribute to goal").selectOption({ label: "🎯 Vacation" });
  await expect(sheet.getByLabel("Contribute to goal")).toHaveValue(/\d+/);
  await page.keyboard.press("Escape");
  await page.getByRole("link", { name: "Goals" }).first().click();
  await expect(page.getByRole("heading", { name: "Goals", level: 1 })).toBeVisible();
  await expect(card).toBeVisible();
  await expect(card).not.toContainText("$100.00");
  await shot(page, "53-goals");
});

test("mobile budget layout", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await login(page);
  await page.goto("/budget");
  await expect(line(page, "Groceries")).toBeVisible();
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
  expect(overflow).toBeLessThanOrEqual(0);
  await page.screenshot({ path: `${shots}/54-budget-mobile.png` });
});
