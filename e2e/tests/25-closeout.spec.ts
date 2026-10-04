import { expect, test, type Page } from "@playwright/test";

const shots = process.env.SCREENSHOT_DIR ?? "test-results/screens";
const shot = async (page: Page, name: string) => {
  await page.waitForTimeout(500);
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

// start-server.sh pins close-out "today" to the 3rd of this month, so last month is open.
const now = new Date();
const prev = new Date(now.getFullYear(), now.getMonth() - 1, 1);
const prevKey = `${prev.getFullYear()}-${String(prev.getMonth() + 1).padStart(2, "0")}`;
const prevName = prev.toLocaleDateString("en-US", { month: "long" });

test("close out last month: review, put leftover toward a goal, next month's risk", async ({ page }) => {
  await login(page);
  // A generous restaurant budget last month guarantees money left over.
  const b = await (await page.request.get(`/api/budget?date=${prevKey}-10`, { headers })).json();
  const rest = b.groups.flatMap((g: { lines: { id: number; name: string }[] }) => g.lines).find((l: { name: string }) => l.name === "Restaurants & Bars");
  await page.request.put("/api/budget/amount", { headers, data: { category_id: rest.id, month: prevKey, amount: "5000" } });
  const goal = await (await page.request.post("/api/goals", { headers, data: { name: "Rainy Day" } })).json();

  await page.reload();
  await expect(page.getByTestId("risk-card")).toBeVisible();
  await expect(page.getByTestId("risk-level")).toHaveText(/Low|Moderate|High|Very high/);
  const banner = page.getByTestId("closeout-banner");
  await expect(banner).toContainText(`Close out ${prevName}`);
  await shot(page, "250-dashboard-risk-closeout");

  await banner.click();
  const dlg = page.getByRole("dialog", { name: `Close out ${prevName}` });
  await expect(dlg.getByTestId("closeout-under")).toContainText("Restaurants & Bars");
  await expect(dlg.getByTestId("closeout-over")).toBeVisible();
  await shot(page, "251-closeout-review");
  await dlg.getByRole("button", { name: "Next" }).click();

  await dlg.getByRole("button", { name: "Add destination" }).click();
  const row = dlg.getByTestId("closeout-allocation");
  await row.getByLabel("Destination").selectOption(`goal:${goal.id}`);
  await expect(row.getByLabel("Destination").locator("option:checked")).toContainText("Rainy Day · goal");
  await row.getByLabel("Amount").fill("25");
  await expect(dlg.getByTestId("closeout-left")).toContainText("stays as cash");
  await shot(page, "252-closeout-leftover");
  await dlg.getByTestId("closeout-submit").click();

  await expect(dlg.getByTestId("closeout-outlook-level")).toHaveText(/Low|Moderate|High|Very high/);
  await expect(dlg.getByTestId("closeout-analysis")).toContainText("You said:");
  await shot(page, "253-closeout-next-month");
  const goals = await (await page.request.get("/api/goals", { headers })).json();
  expect(goals.goals.find((g: { id: number }) => g.id === goal.id).contributed_cents).toBe(2500);

  await dlg.getByRole("button", { name: "Done" }).click();
  await expect(banner).toBeHidden();
  await page.goto("/budget");
  await page.getByTestId("closeout-done").click();
  // Reopening takes the goal money back and brings the banner back.
  await dlg.getByRole("button", { name: "Undo close-out" }).click();
  await expect(dlg).toBeHidden();
  await page.goto("/");
  await expect(page.getByTestId("closeout-banner")).toBeVisible();
  const after = await (await page.request.get("/api/goals", { headers })).json();
  expect(after.goals.find((g: { id: number }) => g.id === goal.id).contributed_cents).toBe(0);
});

test("the risk meter asks the AI why", async ({ page }) => {
  await login(page);
  // This month: $100 for restaurants, already $150 spent → over budget, driving the risk up.
  const b = await (await page.request.get("/api/budget", { headers })).json();
  const rest = b.groups.flatMap((g: { lines: { id: number; name: string }[] }) => g.lines).find((l: { name: string }) => l.name === "Restaurants & Bars");
  await page.request.put("/api/budget/amount", { headers, data: { category_id: rest.id, month: b.month, amount: "100" } });
  const accts = await (await page.request.get("/api/accounts", { headers })).json();
  const cash = accts.accounts.find((a: { builtin: string }) => a.builtin === "paper_cash");
  const t = await (await page.request.post("/api/transactions", { headers, data: { account_id: cash.id, date: b.today, amount: "-150", description: "Bistro" } })).json();
  await page.request.patch(`/api/transactions/${t.id}`, { headers, data: { category_id: rest.id } });
  await page.reload();
  const card = page.getByTestId("risk-card");
  await expect(card).toContainText("Restaurants & Bars");
  await expect(card).toContainText(/\$[\d,]+ over already/);
  await expect(page.getByTestId("risk-level")).toHaveText(/High|Very high/);
  await shot(page, "254-risk-card");
  await card.click();
  const sheet = page.getByRole("dialog", { name: "Discuss Risk of overspending" });
  await expect(sheet.getByTestId("chat-user").first()).toContainText(/risk of overspending/i);
  await expect(sheet.getByTestId("chat-assistant").last()).not.toBeEmpty();
  await shot(page, "255-risk-chat");
});

test("mobile: risk meter and close-out banner fit", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await login(page);
  await expect(page.getByTestId("risk-card")).toBeVisible();
  await expect(page.getByTestId("closeout-banner")).toBeVisible();
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
  expect(overflow).toBeLessThanOrEqual(0);
  await page.getByTestId("closeout-banner").click();
  await expect(page.getByTestId("closeout-dialog")).toBeVisible();
  await shot(page, "256-closeout-mobile");
});
