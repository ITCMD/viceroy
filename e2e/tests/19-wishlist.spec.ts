import { expect, test, type Page } from "@playwright/test";

const shots = process.env.SCREENSHOT_DIR ?? "test-results/screens";
const admin = { email: "admin@example.com", password: "correct horse battery" };
const csrf = { "X-Viceroy-CSRF": "1" };
const shop = "http://127.0.0.1:28433/shop";

async function login(page: Page) {
  await page.goto("/login");
  await page.getByLabel("Email").fill(admin.email);
  await page.getByLabel("Password").fill(admin.password);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page.getByRole("heading", { name: "Dashboard", level: 1 })).toBeVisible();
}

const ymd = (d: Date) => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;

test("wishlist: add from a link, sort, afford card, mark as bought", async ({ page }) => {
  await login(page);
  await page.goto("/wishlist");
  await expect(page.getByRole("heading", { name: "Wishlist", level: 1 })).toBeVisible();
  await expect(page.getByText("Your wishlist is empty")).toBeVisible();

  // Add from a short link: the page is read, tracking parameters dropped.
  await page.getByRole("button", { name: "Add item" }).click();
  const dialog = page.getByTestId("wish-dialog");
  await dialog.getByLabel("Link").fill(`${shop}/short/jsonld`);
  await dialog.getByLabel("Link").blur();
  await expect(dialog.getByLabel("Name")).toHaveValue("Brightside Reading Lamp");
  await expect(dialog.getByLabel("Price")).toHaveValue("49.99");
  await expect(dialog.getByLabel("Link")).toHaveValue(`${shop}/jsonld`);
  await expect(dialog.getByTestId("wish-dialog-image")).toBeVisible();
  await dialog.getByRole("radio", { name: "4 stars" }).click();
  await page.getByRole("button", { name: "Save" }).click();
  const lamp = page.getByTestId("wish-card").filter({ hasText: "Brightside Reading Lamp" });
  await expect(lamp).toContainText("$49.99");
  await expect(lamp.locator("img")).toBeVisible();

  // A store that blocks the request: the user fills in the price.
  await page.getByRole("button", { name: "Add item" }).click();
  await dialog.getByLabel("Link").fill(`${shop}/captcha`);
  await dialog.getByLabel("Link").blur();
  await expect(page.getByTestId("wish-preview-note")).toContainText("didn't share the price");
  await dialog.getByLabel("Name").fill("Noise Cancelling Headphones");
  await dialog.getByLabel("Price").fill("300");
  await page.getByRole("button", { name: "Save" }).click();

  // One without a link, saves money.
  await page.getByRole("button", { name: "Add item" }).click();
  await dialog.getByLabel("Name").fill("Water filter");
  await dialog.getByLabel("Price").fill("20");
  await dialog.getByLabel("Saves money long term").check();
  await page.getByRole("button", { name: "Save" }).click();
  await expect(page.getByTestId("wish-card")).toHaveCount(3);

  // Sorting.
  const titles = () => page.getByTestId("wish-card").locator("button.line-clamp-2").allTextContents();
  // Best value is the default.
  await expect(page.getByRole("button", { name: "Best value" })).toHaveAttribute("aria-pressed", "true");
  await page.getByRole("button", { name: "Newest" }).click();
  await expect.poll(titles).toEqual(["Water filter", "Noise Cancelling Headphones", "Brightside Reading Lamp"]);
  await page.getByRole("button", { name: "Price", exact: true }).click();
  await expect.poll(titles).toEqual(["Water filter", "Brightside Reading Lamp", "Noise Cancelling Headphones"]);
  await page.getByRole("button", { name: "Best value" }).click();
  await expect(page.getByTestId("wish-score").first()).toContainText("3★ × 1 person ÷ $20 × 1.5");

  // Fund the goal: $100 already saved, $50/month with nothing in yet → $150 by month end.
  const wl = await (await page.request.get("/api/wishlist")).json();
  const goal = wl.afford.goal_id;
  await page.request.patch(`/api/goals/${goal}`, { headers: csrf, data: { starting: "100" } });
  await page.request.put("/api/budget/amount", { headers: csrf, data: { goal_id: goal, month: ymd(new Date()).slice(0, 7), amount: "50" } });
  await page.reload();
  const card = page.getByTestId("wishlist-afford");
  await expect(card).toContainText("$100.00");
  await expect(card).toContainText("$150.00");
  // Best value: filter ($20) and lamp ($49.99) now; $30.01 left now, $80.01 by month end; headphones don't fit.
  await expect(page.getByTestId("wish-card").filter({ hasText: "Water filter" })).toContainText("Affordable now");
  await expect(lamp).toContainText("Affordable now");
  await expect(page.getByTestId("wish-card").filter({ hasText: "Headphones" })).not.toContainText("Affordable");
  await page.screenshot({ path: `${shots}/19-wishlist.png`, fullPage: true });

  // Stars change in place.
  await lamp.getByRole("radio", { name: "5 stars" }).click();
  await expect(lamp.getByRole("radio", { name: "5 stars" })).toHaveAttribute("aria-checked", "true");

  // Mark as purchased, linking the purchase transaction: spent from the goal.
  const acct = await (await page.request.post("/api/accounts", { headers: csrf, data: { name: "Wish Cash", type: "cash", balance: "500" } })).json();
  await page.request.post("/api/transactions", { headers: csrf, data: { account_id: acct.id, date: ymd(new Date()), amount: "-49.99", description: "Brightside Home" } });
  await lamp.getByRole("button", { name: "Mark as purchased" }).click();
  const bought = page.getByTestId("wish-bought-dialog");
  await expect(bought.getByLabel("Purchased on")).toBeVisible(); // linking is optional
  await bought.getByRole("switch", { name: "Link the purchase transaction" }).click();
  await bought.getByLabel("Search transactions").fill("Brightside");
  await bought.getByRole("button", { name: /Brightside Home/ }).click();
  await page.getByRole("dialog").getByRole("button", { name: "Mark as purchased" }).click();
  await expect(page.getByTestId("wish-card")).toHaveCount(2);
  await expect(card).toContainText("$50.01");
  await page.getByRole("button", { name: "Purchased (1)" }).click();
  await expect(page.getByTestId("wishlist-bought")).toContainText("Brightside Reading Lamp");

  // The transaction says it was spent from the goal.
  await page.goto("/transactions");
  await page.getByText("Brightside Home").first().click();
  await expect(page.getByRole("switch", { name: "Spent from this goal" })).toBeChecked();
  await page.keyboard.press("Escape");

  // Undo puts it back.
  await page.goto("/wishlist");
  await page.getByRole("button", { name: "Purchased (1)" }).click();
  await page.getByRole("button", { name: "Put Brightside Reading Lamp back on the list" }).click();
  await expect(page.getByTestId("wish-card")).toHaveCount(3);
  await expect(card).toContainText("$100.00");

  // Pasting a link on the page opens the dialog with it.
  await page.evaluate((u) => {
    const dt = new DataTransfer();
    dt.setData("text", u);
    document.body.dispatchEvent(new ClipboardEvent("paste", { clipboardData: dt, bubbles: true }));
  }, `${shop}/textonly`);
  await expect(dialog.getByLabel("Name")).toHaveValue("Garden Bench - Woodworks");
  await expect(dialog.getByLabel("Price")).toHaveValue("219");
  await page.getByRole("button", { name: "Cancel" }).click();
});

test("budget: Actual and View transactions open the category's transactions; tab strip has no scrollbar", async ({ page }) => {
  await login(page);
  const acct = await (await page.request.post("/api/accounts", { headers: csrf, data: { name: "Budget Link Cash", type: "cash", balance: "500" } })).json();
  const cats = await (await page.request.get("/api/categories")).json();
  const groceries = cats.groups.flatMap((g: { categories: { id: number; name: string }[] }) => g.categories).find((c: { name: string }) => c.name === "Groceries");
  const t = await (await page.request.post("/api/transactions", { headers: csrf, data: { account_id: acct.id, date: ymd(new Date()), amount: "-23.45", description: "Corner Grocer" } })).json();
  await page.request.patch(`/api/transactions/${t.id}`, { headers: csrf, data: { category_id: groceries.id } });
  await page.request.post("/api/transactions", { headers: csrf, data: { account_id: acct.id, date: ymd(new Date()), amount: "-9", description: "Not Groceries Co" } });

  await page.goto("/budget");
  const line = page.getByTestId("budget-line").filter({ hasText: "Groceries" });
  await line.getByTestId("budget-line-actual").click();
  await expect(page).toHaveURL(/\/transactions\?/);
  await expect(page.getByTestId("txn-link-filter")).toContainText("Groceries");
  await expect(page.getByText("Corner Grocer")).toBeVisible();
  await expect(page.getByText("Not Groceries Co")).toHaveCount(0);
  await page.getByRole("button", { name: "Clear filter" }).click();
  await expect(page.getByText("Not Groceries Co")).toBeVisible();

  // From the editor.
  await page.goto("/budget");
  await line.click();
  await page.getByRole("button", { name: "View transactions" }).click();
  await expect(page.getByTestId("txn-link-filter")).toContainText("Groceries");
  await expect(page.getByText("Corner Grocer")).toBeVisible();

  // The tab strip doesn't scroll vertically (it used to show a scrollbar on Windows).
  await page.goto("/settings");
  const tabs = page.getByRole("tablist").first();
  expect(await tabs.evaluate((el) => el.scrollHeight <= el.clientHeight)).toBe(true);
});
