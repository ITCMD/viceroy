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

test("payoff schedule per debt, saved plan, and Debt Repayment budget lines", async ({ page }) => {
  await login(page);
  const card = await (await page.request.post("/api/accounts", { headers, data: { name: "Store Card", type: "credit_card", balance: "600" } })).json();
  await page.request.patch(`/api/accounts/${card.id}`, { headers, data: { apr: "29.99", min_payment: "25" } });
  // Debts other specs created need terms for a plan to exist.
  const rep = await (await page.request.get("/api/reports/debt", { headers })).json();
  for (const d of rep.debts) {
    if (d.apr_source === "missing" || d.min_payment_source === "missing") {
      await page.request.patch(`/api/accounts/${d.account_id}`, { headers, data: { apr: "5", min_payment: "300" } });
    }
  }

  // Debt Free Future: each debt lists what the plan pays it, month by month.
  await page.goto("/reports");
  await page.getByRole("tab", { name: "Debt Free Future" }).click();
  await page.getByLabel("Extra each month").fill("150");
  await expect(page.getByTestId("debt-plan-saved")).toContainText("Plan saved");
  const item = page.getByTestId("debt-order-item").filter({ hasText: "Store Card" });
  await expect(item.getByTestId("debt-schedule")).toContainText("$");
  await page.getByRole("button", { name: "Show month by month" }).click();
  await expect(page.getByTestId("debt-monthly")).toContainText("Store Card");
  await shot(page, "241-debt-schedule");
  // The plan is saved on the server, not just in this browser.
  await page.waitForTimeout(800);
  const saved = await (await page.request.get("/api/reports/debt", { headers })).json();
  expect(saved.extra).toBe(15000);
  await page.reload();
  await page.getByRole("tab", { name: "Debt Free Future" }).click();
  await expect(page.getByLabel("Extra each month")).toHaveValue("150");

  // Budget: Debt Repayment opens into one line per debt.
  await page.goto("/budget");
  const parent = page.getByTestId("budget-line").filter({ hasText: "Debt Repayment" });
  await expect(parent).toContainText("debts");
  if ((await parent.getAttribute("aria-expanded")) !== "true") await parent.click();
  const sub = page.getByTestId("budget-subline").filter({ hasText: "Store Card" });
  await expect(sub).toBeVisible();
  await sub.click();
  const dialog = page.getByRole("dialog");
  await expect(dialog.getByTestId("budget-debt-min")).toContainText("$25.00");
  await expect(dialog.getByTestId("budget-debt-plan")).toContainText("plan");
  await expect(dialog.getByTestId("budget-debt-breakdown")).toContainText("paid down");
  await shot(page, "242-debt-budget-dialog");
  await dialog.getByTestId("budget-debt-min").getByRole("button", { name: "Use" }).click();
  await expect(dialog.getByTestId("budget-amount")).toHaveValue("25");
  await dialog.getByRole("button", { name: "Save" }).click();
  await expect(dialog).toBeHidden();
  await expect(sub).toContainText("$25.00");
  await shot(page, "243-debt-budget-lines");

  // Settings › Budget: count the whole payment instead.
  await page.goto("/settings");
  await page.getByTestId("debt-actual-setting").selectOption("paid");
  await expect.poll(async () => (await (await page.request.get("/api/settings", { headers })).json()).budget.debt_actual).toBe("paid");
  await page.getByTestId("debt-actual-setting").selectOption("net");
});

test("chat about the budget, and what AI costs", async ({ page }) => {
  await login(page);
  await page.goto("/budget");
  await page.getByTestId("budget-chat").click();
  await expect(page.getByRole("dialog")).toContainText("Discuss your budget");
  await page.getByRole("button", { name: "How am I doing this month?" }).click();
  await expect(page.getByTestId("chat-assistant").last()).toContainText("Looking at your Budget");
  // Every request's cost (the fake OpenRouter charges $0.0012) adds up per chat...
  await expect(page.getByTestId("chat-cost")).toContainText("This chat: $0.00");
  await shot(page, "244-budget-chat");
  await page.keyboard.press("Escape");

  // ...and per month in Settings › AI.
  await page.goto("/settings#ai");
  const cost = page.getByTestId("ai-cost");
  await expect(page.getByTestId("ai-cost-month")).toHaveText(/^\$\d+\.\d{2,4}\+?$/);
  await cost.getByRole("button", { name: "Show breakdown" }).click();
  await expect(cost).toContainText("Chat");
  await cost.scrollIntoViewIfNeeded();
  await shot(page, "245-ai-cost");
});
