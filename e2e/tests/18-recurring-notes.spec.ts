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

const ymd = (d: Date) => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
const daysAgo = (n: number) => {
  const d = new Date();
  d.setDate(d.getDate() - n);
  return ymd(d);
};

test("recurring: mark from a transaction, track a suggestion, edit, budget shows upcoming; category list scrolls; net worth notes", async ({ page }) => {
  await login(page);
  const acct = await (await page.request.post("/api/accounts", { headers: csrf, data: { name: "Recurring Cash", type: "cash", balance: "500" } })).json();
  const cats = await (await page.request.get("/api/categories")).json();
  const rest = cats.groups.flatMap((g: { categories: { id: number; name: string }[] }) => g.categories).find((c: { name: string }) => c.name === "Restaurants & Bars");
  const add = async (date: string, amount: string, description: string) => {
    const t = await (await page.request.post("/api/transactions", { headers: csrf, data: { account_id: acct.id, date, amount, description } })).json();
    await page.request.patch(`/api/transactions/${t.id}`, { headers: csrf, data: { category_id: rest.id } });
  };
  await add(daysAgo(61), "-12.99", "Streambox Plus");
  await add(daysAgo(31), "-12.99", "Streambox Plus");
  await add(daysAgo(20), "-40", "Supper Club Dues"); // outside the 6-day window of a payment due today

  // The category list scrolls with the mouse wheel inside the transaction sheet.
  await page.goto("/transactions");
  await page.getByText("Supper Club Dues").first().click();
  await page.getByLabel("Category", { exact: true }).click();
  const list = page.getByRole("listbox", { name: "Category" });
  await expect(list).toBeVisible();
  await list.hover();
  await page.mouse.wheel(0, 400);
  await expect.poll(() => list.evaluate((el) => el.scrollTop)).toBeGreaterThan(0);
  await page.keyboard.press("Escape");

  // Mark as recurring, due today, so it's upcoming.
  await page.getByRole("button", { name: "Mark as recurring" }).click();
  const dialog = page.getByTestId("recurring-dialog");
  await expect(dialog.getByLabel("Amount", { exact: true })).toHaveValue("40");
  await dialog.getByLabel("Next due").fill(ymd(new Date()));
  await page.getByRole("button", { name: "Save" }).click();
  await expect(page.getByTestId("txn-recurring")).toContainText("Recurring: Supper Club Dues · Monthly");
  await page.keyboard.press("Escape");

  // Recurring tab: the suggestion, the tracked item, the calendar.
  await page.goto("/recurring");
  await expect(page.getByTestId("recurring-list")).toContainText("Supper Club Dues");
  await expect(page.getByTestId("before-payday")).toContainText("Supper Club Dues");
  const sug = page.getByTestId("recurring-suggestions").getByTestId("recurring-row").filter({ hasText: "Streambox" });
  await expect(sug).toContainText("seen 2×");
  await sug.getByRole("button", { name: "Track" }).click();
  await expect(page.getByRole("dialog", { name: "Track recurring" })).toBeVisible();
  await page.getByRole("button", { name: "Save" }).click();
  await expect(page.getByTestId("recurring-list")).toContainText("Streambox");
  await expect(page.getByTestId(`cal-${ymd(new Date())}`)).toContainText("Supper Club Dues");
  await page.screenshot({ path: `${shots}/18-recurring.png`, fullPage: true });

  // Edit it.
  await page.getByTestId("recurring-list").getByText("Supper Club Dues").click();
  await page.getByLabel("Name").fill("Supper club");
  await page.getByRole("button", { name: "Save" }).click();
  await expect(page.getByTestId("recurring-list")).toContainText("Supper club");

  // Budget: the line gets a blue upcoming segment.
  await page.goto("/budget");
  const line = page.getByTestId("budget-line").filter({ hasText: "Restaurants & Bars" });
  await expect(line).toContainText(/\+\$\d+\.\d\d soon/); // Supper club, plus Streambox when it falls in the same week
  await expect(line.getByTestId("budget-upcoming")).toBeVisible();

  // Net worth note: right-click the chart, name it, pick an icon.
  await page.goto("/");
  const chart = page.getByTestId("area-chart").first();
  await chart.scrollIntoViewIfNeeded();
  const box = (await chart.boundingBox())!;
  await page.mouse.click(box.x + box.width * 0.6, box.y + box.height / 2, { button: "right" });
  const note = page.getByTestId("networth-note-dialog");
  await expect(note).toBeVisible();
  await note.getByLabel("Name").fill("Moved to the city");
  await note.getByRole("button", { name: "🎉" }).click();
  await page.getByRole("button", { name: "Save" }).click();
  await expect(note).toBeHidden();
  await expect(chart.locator("text", { hasText: "🎉" })).toBeVisible();
  const notes = await (await page.request.get("/api/networth/annotations")).json();
  expect(notes.annotations.some((n: { label: string }) => n.label === "Moved to the city")).toBe(true);
  await page.screenshot({ path: `${shots}/18-networth-note.png` });
});
