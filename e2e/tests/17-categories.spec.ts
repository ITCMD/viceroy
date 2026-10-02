import { expect, test, type Page } from "@playwright/test";

const shots = process.env.SCREENSHOT_DIR ?? "test-results/screens";
const admin = { email: "admin@example.com", password: "correct horse battery" };
const csrf = { "X-Viceroy-CSRF": "1" };

async function login(page: Page) {
  await page.goto("/login");
  await page.getByLabel("Email").fill(admin.email);
  await page.getByLabel("Password").fill(admin.password);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page.getByRole("heading", { name: "Dashboard", level: 1 })).toBeVisible();
}

const names = async (page: Page, kind: string) => page.getByTestId(`category-group-${kind}`).getByTestId("category-row").allTextContents();

test("categories: reorder, move to Non-monthly, and non-monthly rolls over", async ({ page }) => {
  await login(page);
  await page.goto("/settings");
  await page.getByRole("tab", { name: "Categories" }).click();
  await expect(page).toHaveURL(/tab=categories/);
  const nonMonthly = page.getByTestId("category-group-non_monthly");
  await expect(nonMonthly).toContainText("carry over into the next month");

  // Move Gifts from Flexible to Non-monthly, then to the top of it.
  await page.getByLabel("Group for Gifts").selectOption({ label: "Non-monthly" });
  await expect(nonMonthly.getByTestId("category-row").last()).toContainText("Gifts");
  for (let i = (await names(page, "non_monthly")).length - 1; i > 0; i--) await page.getByRole("button", { name: "Move Gifts up" }).click();
  await expect(nonMonthly.getByTestId("category-row").first()).toContainText("Gifts");
  expect((await names(page, "flexible")).some((n) => n.includes("Gifts"))).toBe(false);

  // Drag Coffee Shops above Groceries.
  const flexible = page.getByTestId("category-group-flexible");
  await flexible.getByTestId("category-row").filter({ hasText: "Coffee Shops" }).dragTo(flexible.getByTestId("category-row").filter({ hasText: "Groceries" }), {
    targetPosition: { x: 40, y: 4 },
  });
  await expect(flexible.getByTestId("category-row").first()).toContainText("Coffee Shops");
  await page.screenshot({ path: `${shots}/17-categories.png`, fullPage: true });

  // It's saved: a reload keeps the order.
  await page.reload();
  await expect(nonMonthly.getByTestId("category-row").first()).toContainText("Gifts");
  await expect(flexible.getByTestId("category-row").first()).toContainText("Coffee Shops");

  // Budget $100 for Gifts last month with nothing spent: this month carries it over.
  const cats = await (await page.request.get("/api/categories")).json();
  const gifts = cats.groups.flatMap((g: { categories: { id: number; name: string }[] }) => g.categories).find((c: { name: string }) => c.name === "Gifts");
  const d = new Date();
  d.setDate(1);
  d.setMonth(d.getMonth() - 1);
  const lastMonth = `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}`;
  const res = await page.request.put("/api/budget/amount", {
    headers: csrf,
    data: { category_id: gifts.id, month: lastMonth, amount: "100", apply_forward: true },
  });
  expect(res.ok()).toBe(true);
  await page.goto("/budget");
  const group = page.getByTestId("budget-group-non_monthly");
  await expect(group).toContainText("Rolls over monthly");
  const line = group.getByTestId("budget-line").first();
  await expect(line).toContainText("Gifts");
  await expect(line.getByTestId("budget-rollover")).toHaveText("↻ +$100.00");
  await line.click();
  await expect(page.getByTestId("budget-rollover-note")).toContainText("Unspent in earlier months: $100.00");
  await page.screenshot({ path: `${shots}/17-rollover.png` });
});
