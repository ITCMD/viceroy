import { expect, test, type Page } from "@playwright/test";

const shots = process.env.SCREENSHOT_DIR ?? "test-results/screens";
const fakeImap = "http://127.0.0.1:28432";
const admin = { email: "admin@example.com", password: "correct horse battery" };

async function login(page: Page) {
  await page.goto("/login");
  await page.getByLabel("Email").fill(admin.email);
  await page.getByLabel("Password").fill(admin.password);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page.getByRole("heading", { name: "Dashboard", level: 1 })).toBeVisible();
}

const longDate = (days: number) => {
  const d = new Date();
  d.setDate(d.getDate() + days);
  return d.toLocaleDateString("en-US", { month: "long", day: "numeric", year: "numeric" });
};
const shortDate = (days: number) => {
  const d = new Date();
  d.setDate(d.getDate() + days);
  return d.toLocaleDateString("en-US", { month: "short", day: "numeric" });
};

function email(id: string, subject: string, body: string) {
  return [
    "From: CardCo <alerts@cardco.example>",
    `Subject: ${subject}`,
    `Date: ${new Date().toUTCString()}`,
    `Message-ID: <${id}@cardco.example>`,
    "Content-Type: text/plain",
    "",
    body,
    "",
  ].join("\r\n");
}

test("AI reads unmatched bank emails: payment due, security alert, purchase hint", async ({ page }) => {
  await login(page);
  await page.goto("/settings");

  // The AI card shows what's set up and turns reading on for the mailbox from the email spec.
  const aiCard = page.locator("section", { has: page.getByRole("heading", { name: "AI", exact: true }) });
  await expect(aiCard).toContainText("Chat with your budget");
  await expect(aiCard).toContainText("Reading bank emails");
  await expect(aiCard.getByTestId("ai-config-snippet")).toHaveCount(0);
  await aiCard.getByTestId("ai-mailbox-row").first().getByRole("button", { name: "Turn on" }).click();
  const dialog = page.getByRole("dialog");
  const toggle = dialog.getByRole("switch", { name: /Read unmatched emails with AI/ });
  await expect(toggle).toBeEnabled();
  await toggle.click();
  await dialog.getByLabel("Only from these senders").fill("cardco.example");
  await dialog.getByRole("button", { name: "Save" }).click();
  await expect(dialog).toHaveCount(0);
  await expect(aiCard.getByTestId("ai-mailbox-row").first()).toContainText("From cardco.example");
  await aiCard.screenshot({ path: `${shots}/11-ai-card.png` });

  const req = page.request;
  await req.post(`${fakeImap}/deliver`, {
    data: email("due-1", "Your payment is due soon", `Your Quicksilver card ending in 3333 has a payment due of $245.10 on ${longDate(2)}. Minimum payment $35.00.`),
  });
  await req.post(`${fakeImap}/deliver`, {
    data: email("sec-1", "Unusual activity on your card", "We noticed suspicious activity on your card ending in 3333. Please review it."),
  });
  await req.post(`${fakeImap}/deliver`, {
    data: email("buy-1", "Purchase alert", "A purchase of $9.99 was made with your card ending in 3333."),
  });

  await expect
    .poll(async () => (await (await req.get("/api/notifications")).json()).notifications.map((n: { title: string }) => n.title), { timeout: 20_000 })
    .toEqual(expect.arrayContaining([expect.stringContaining("Security alert · Quicksilver"), expect.stringContaining("payment due in 2 days")]));

  // The card shows the payment due.
  await page.goto("/accounts");
  const card = page.getByTestId("account-row").filter({ hasText: "Quicksilver" });
  await expect(card.getByTestId("bill-badges")).toContainText(`Due ${shortDate(2)}`);
  await card.screenshot({ path: `${shots}/11-bill-badge.png` });

  // Bank messages have their own tab under the bell.
  await page.getByRole("button", { name: /^Notifications/ }).first().click();
  await page.getByRole("button", { name: /From your bank/ }).click();
  const list = page.getByTestId("notification-list");
  await expect(list).toContainText("Security alert · Quicksilver");
  await expect(list).toContainText(`Payment due ${shortDate(2)} · Quicksilver`);
  await page.screenshot({ path: `${shots}/11-bank-tab.png` });
  await page.keyboard.press("Escape");

  // The purchase alert has no filter: it stays in Emails to review with the AI's hint.
  await page.goto("/settings");
  const row = page.getByTestId("review-email-row").filter({ hasText: "Purchase alert" });
  await expect(row.getByTestId("email-ai-note")).toContainText("looks like a purchase alert");
  await expect(page.getByTestId("review-email-row").filter({ hasText: "Unusual activity" })).toHaveCount(0);
});
