import { expect, test, type Page } from "@playwright/test";

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

const thisMonth = () => {
  const d = new Date();
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}`;
};

test("notification settings, test alert and the bell", async ({ page }) => {
  await login(page);
  await page.goto("/settings");
  const card = page.locator("section", { has: page.getByRole("heading", { name: "Notifications" }) });
  await expect(card.getByText("Alert me when")).toBeVisible();

  // Toggles save right away and survive a reload.
  const over = card.getByRole("switch", { name: "A category goes over budget" });
  await expect(over).toBeChecked();
  await over.click();
  await expect(over).not.toBeChecked();
  const amount = card.getByLabel("Large transaction ($)");
  await expect(amount).toHaveValue("500");
  await amount.fill("250");
  await card.getByRole("button", { name: "Save" }).click();
  await expect(card.getByRole("button", { name: "Save" })).toHaveCount(0);
  await page.reload();
  await expect(card.getByRole("switch", { name: "A category goes over budget" })).not.toBeChecked();
  await expect(card.getByLabel("Large transaction ($)")).toHaveValue("250");
  await card.getByRole("switch", { name: "A category goes over budget" }).click();
  await expect(card.getByRole("switch", { name: "A category goes over budget" })).toBeChecked();

  // No device has push on, but the test alert still lands under the bell.
  await card.getByRole("button", { name: "Send test" }).click();
  await expect(card.getByRole("status")).toContainText("No devices have push turned on yet");
  // Earlier specs' data already raised a few alerts, so only check that some are unread.
  const bell = page.getByRole("button", { name: /Notifications \(\d+ unread\)/ });
  await expect(bell).toBeVisible();
  await bell.click();
  await expect(page.getByTestId("notification-list")).toContainText("Test notification");
  await shot(page, "07-bell");
  await page.keyboard.press("Escape");
  await expect(page.getByRole("button", { name: "Notifications", exact: true })).toBeVisible();

  // A device registered through the API shows in the list and can be removed.
  await page.request.post("/api/notifications/subscriptions", {
    headers,
    data: { endpoint: "https://push.example.invalid/e2e", keys: { p256dh: "BK", auth: "AU" } },
  });
  await page.reload();
  await expect(card.getByTestId("push-device")).toHaveCount(1);
  await shot(page, "07-notification-settings");
  await card.getByRole("button", { name: "Remove device" }).click();
  await expect(card.getByTestId("push-device")).toHaveCount(0);
});

test("over-budget alert after a manual transaction", async ({ page }) => {
  await login(page);
  const req = page.request;
  const budget = await (await req.get("/api/budget")).json();
  let coffee = 0;
  for (const g of budget.groups) for (const l of g.lines) if (l.name === "Coffee Shops") coffee = l.id;
  expect(coffee).toBeGreaterThan(0);
  await req.put("/api/budget/amount", { headers, data: { category_id: coffee, month: thisMonth(), amount: "20", apply_forward: false } });
  const acct = await (await req.post("/api/accounts", { headers, data: { name: "Alert Wallet", type: "cash", balance: "100" } })).json();
  const d = new Date();
  const today = `${thisMonth()}-${String(d.getDate()).padStart(2, "0")}`;
  const txn = await (await req.post("/api/transactions", { headers, data: { account_id: acct.id, date: today, amount: "-32.50", description: "Blue Bottle" } })).json();
  await req.patch(`/api/transactions/${txn.id}`, { headers, data: { category_id: coffee } });

  await expect
    .poll(async () => (await (await req.get("/api/notifications")).json()).notifications.map((n: { title: string }) => n.title), { timeout: 15_000 })
    .toContain("Over budget: Coffee Shops");
  await page.reload();
  await page.getByRole("button", { name: /Notifications \(\d+ unread\)/ }).click();
  const item = page.getByTestId("notification-list").getByRole("button", { name: /Over budget: Coffee Shops/ });
  await expect(item).toContainText("$32.50 spent of $20.00 this month ($12.50 over).");
  await item.click();
  await expect(page.getByRole("heading", { name: "Budget", level: 1 })).toBeVisible();
});

test("chat with your budget", async ({ page }) => {
  await login(page);
  await page.getByRole("button", { name: "Chat with your budget" }).click();
  const sheet = page.getByRole("dialog", { name: "Chat with your budget" });
  await expect(sheet.getByText("Ask about your money")).toBeVisible();

  await sheet.getByRole("button", { name: "How am I doing on my budget this month?" }).click();
  const answer = sheet.getByTestId("chat-assistant").last();
  await expect(answer).toContainText("I checked budget_status");
  await expect(answer).toContainText("Checked your budget");
  await expect(answer.locator("strong", { hasText: "budget_status" })).toBeVisible();
  await expect(answer.locator("li")).not.toHaveCount(0);
  await expect(sheet.getByTestId("chat-user")).toHaveCount(1);

  await sheet.getByLabel("Message").fill("thanks!");
  await sheet.getByLabel("Message").press("Enter");
  await expect(sheet.getByTestId("chat-assistant").last()).toContainText("You said: thanks!");
  await expect(sheet.getByTestId("chat-user")).toHaveCount(2);
  await shot(page, "07-chat");

  // New chat, then reopen the first one from history.
  await sheet.getByRole("button", { name: "New chat" }).click();
  await expect(sheet.getByText("Ask about your money")).toBeVisible();
  await sheet.getByRole("button", { name: "Past chats" }).click();
  await page.getByTestId("chat-thread").filter({ hasText: "How am I doing" }).getByRole("button").first().click();
  await expect(sheet.getByTestId("chat-user")).toHaveCount(2);
  await expect(sheet.getByTestId("chat-assistant").first()).toContainText("Checked your budget");

  // Delete it.
  await sheet.getByRole("button", { name: "Past chats" }).click();
  await page.getByRole("button", { name: /Delete “How am I doing/ }).click();
  await expect(sheet.getByText("Ask about your money")).toBeVisible();
});

test("mobile: bell in the header and full-width chat", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await login(page);
  await expect(page.getByRole("button", { name: /^Notifications/ })).toBeVisible();
  await page.getByRole("button", { name: "Chat", exact: true }).click();
  const sheet = page.getByRole("dialog", { name: "Chat with your budget" });
  await sheet.getByLabel("Message").fill("show my accounts");
  await sheet.getByRole("button", { name: "Send" }).click();
  await expect(sheet.getByTestId("chat-assistant").last()).toContainText("Looked at your accounts");
  const box = await sheet.boundingBox();
  expect(box?.width).toBe(390);
  await shot(page, "07-chat-mobile");
});
