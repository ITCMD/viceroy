import { expect, test, type Page } from "@playwright/test";

const shots = process.env.SCREENSHOT_DIR ?? "test-results/screens";
const shot = (page: Page, name: string) => page.screenshot({ path: `${shots}/${name}.png` });
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

test("categories: add, rename and delete a custom category; exclude one from pacing", async ({ page }) => {
  await login(page);
  await page.goto("/settings?tab=categories");
  const flexible = page.getByTestId("category-group-flexible");
  await page.getByRole("button", { name: "Add category to Flexible" }).click();
  const dlg = page.getByTestId("category-dialog");
  // The icon is picked from a searchable emoji grid.
  await dlg.getByTestId("icon-picker").click();
  await shot(page, "171-icon-picker");
  await page.getByLabel("Search emoji").fill("dog");
  await page.getByRole("option", { name: "dog" }).click();
  await expect(dlg.getByTestId("icon-picker")).toHaveText("🐶");
  await dlg.getByLabel("Name").fill("Dog walker");
  await page.getByRole("button", { name: "Save" }).click();
  await expect(flexible.getByTestId("category-row").last()).toContainText("Dog walker");

  // A duplicate name is refused.
  await page.getByRole("button", { name: "Add category to Fixed" }).click();
  await page.getByTestId("category-dialog").getByLabel("Name").fill("dog walker");
  await page.getByRole("button", { name: "Save" }).click();
  await expect(page.getByRole("dialog")).toContainText("There's already a category called “Dog walker”");
  await page.getByRole("button", { name: "Cancel" }).click();

  await page.getByRole("button", { name: "Edit Dog walker" }).click();
  await page.getByTestId("category-dialog").getByLabel("Name").fill("Dog care");
  // Or uploaded as an image.
  await page.getByTestId("category-dialog").getByTestId("icon-picker").click();
  const png = Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==", "base64");
  await page.getByTestId("icon-upload").setInputFiles({ name: "paw.png", mimeType: "image/png", buffer: png });
  await expect(page.getByTestId("category-dialog").getByTestId("icon-picker").locator("img")).toBeVisible();
  await page.getByRole("button", { name: "Save" }).click();
  await expect(flexible).toContainText("Dog care");
  await expect(flexible.getByTestId("category-row").filter({ hasText: "Dog care" }).locator("img")).toHaveAttribute("src", /\/api\/categories\/\d+\/icon\?v=/);

  // Exclude it from pacing in the budget editor.
  await page.goto("/budget");
  const line = page.getByTestId("budget-line").filter({ hasText: "Dog care" });
  await line.click();
  await page.getByLabel("Exclude from pacing").check();
  await page.getByRole("button", { name: "Save" }).click();
  await expect(line).toContainText("Not paced");

  // Delete it.
  await page.goto("/settings?tab=categories");
  await page.getByRole("button", { name: "Edit Dog care" }).click();
  await page.getByRole("button", { name: "Delete category" }).click();
  await expect(page.getByTestId("category-delete")).toContainText("No transactions use Dog care");
  await page.getByRole("button", { name: "Delete category" }).click();
  await expect(flexible).not.toContainText("Dog care");
});
