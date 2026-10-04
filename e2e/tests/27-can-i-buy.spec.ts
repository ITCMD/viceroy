import { expect, test, type Page } from "@playwright/test";

const shots = process.env.SCREENSHOT_DIR ?? "test-results/screens";
const admin = { email: "admin@example.com", password: "correct horse battery" };
const headers = { "X-Viceroy-CSRF": "1" };

async function login(page: Page) {
  await page.goto("/login");
  await page.getByLabel("Email").fill(admin.email);
  await page.getByLabel("Password").fill(admin.password);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page.getByRole("heading", { name: "Dashboard", level: 1 })).toBeVisible();
}

test("can I buy it: the budget and pace answer, the AI only reads the request", async ({ page }) => {
  await login(page);
  const budget = await (await page.request.get("/api/budget")).json();
  let coffee = 0;
  for (const g of budget.groups) for (const l of g.lines) if (l.name === "Coffee Shops") coffee = l.id;
  const d = new Date();
  const month = `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}`;
  // Plenty of room, spread evenly: a coffee is a clear yes.
  await page.request.put("/api/budget/amount", { headers, data: { category_id: coffee, month, amount: "1000", apply_forward: false } });
  await page.request.put(`/api/budget/categories/${coffee}/chunk`, { headers, data: { kind: "even" } });

  await page.getByRole("button", { name: "Can I buy?" }).click();
  const dialog = page.getByRole("dialog", { name: "Can I buy it?" });
  await dialog.getByRole("button", { name: "Coffee", exact: true }).click();
  const answer = dialog.getByTestId("can-i-buy-answer");
  await expect(answer).toHaveAttribute("data-answer", "yes");
  await expect(answer).toContainText("You're under budget in Coffee Shops.");
  await expect(dialog.getByLabel("Price")).toHaveValue(/^\d+\.\d{2}$/);
  await page.screenshot({ path: `${shots}/27-can-i-buy-yes.png` });

  // A real price that blows the budget: no AI round trip, and the answer is no.
  await dialog.getByLabel("Price").fill("2000");
  await dialog.getByRole("button", { name: "Check again" }).click();
  await expect(answer).toHaveAttribute("data-answer", "no");
  await expect(answer).toContainText("Better not");
  await expect(answer).toContainText("over budget");
  await expect(answer).toContainText("Price: the price you entered.");

  // Phone layout.
  await page.keyboard.press("Escape");
  await page.setViewportSize({ width: 390, height: 844 });
  await page.getByRole("button", { name: "Can I buy?" }).click();
  await dialog.getByLabel("What do you want to buy?").fill("bagels from the bagel shop");
  await dialog.getByRole("button", { name: "Check", exact: true }).click();
  await expect(answer).toContainText("Restaurants & Bars");
  await page.screenshot({ path: `${shots}/27-can-i-buy-mobile.png` });
});
