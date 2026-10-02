import { expect, test, type Page } from "@playwright/test";

const shots = process.env.SCREENSHOT_DIR ?? "test-results/screens";
const fakeImap = "http://127.0.0.1:28432";
const admin = { email: "admin@example.com", password: "correct horse battery" };
const csrf = { headers: { "X-Viceroy-CSRF": "1" } };

async function login(page: Page) {
  await page.goto("/login");
  await page.getByLabel("Email").fill(admin.email);
  await page.getByLabel("Password").fill(admin.password);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page.getByRole("heading", { name: "Dashboard", level: 1 })).toBeVisible();
}

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

const today = new Date().toLocaleDateString("en-US", { month: "long", day: "numeric", year: "numeric" });

test("a balance summary from the bank updates the card's balance, and can do so every time", async ({ page }) => {
  await login(page);
  const req = page.request;
  const balance = async () => {
    const { accounts } = await (await req.get("/api/accounts")).json();
    return accounts.find((a: { name: string }) => a.name.includes("Quicksilver")).balance_cents;
  };
  const checkMail = async () => {
    for (const m of await (await req.get("/api/email/mailboxes")).json()) await req.post(`/api/email/mailboxes/${m.id}/check`, csrf);
  };

  await req.post(`${fakeImap}/deliver`, {
    data: email("bal-1", "Your requested balance summary", `Quicksilver (3333) has a balance of $14.99 as of ${today}.\n\nThanks for being a customer.`),
  });
  await expect
    .poll(
      async () => {
        await checkMail();
        return (await (await req.get("/api/notifications")).json()).notifications.map((n: { title: string }) => n.title);
      },
      { timeout: 30_000 },
    )
    .toContain("Balance · Quicksilver Card (3333)");

  // The notice opens the email with what Viceroy can do about it.
  await page.reload();
  await page.getByRole("button", { name: /^Notifications/ }).first().click();
  await page.getByRole("button", { name: /From your bank/ }).click();
  await page.getByTestId("notification-list").getByRole("button", { name: /Balance · Quicksilver/ }).click();
  const dialog = page.getByRole("dialog");
  await expect(dialog).toContainText("Your requested balance summary");
  const actions = dialog.getByTestId("notice-actions");
  const update = actions.getByRole("button", { name: "Update Quicksilver Card (3333) balance to $14.99" });
  await expect(update).toBeVisible();
  await actions.getByRole("switch", { name: /Always do this/ }).click();
  await page.screenshot({ path: `${shots}/16-balance-notice.png` });
  await update.click();
  await expect(dialog.getByTestId("notice-applied")).toContainText("Set Quicksilver Card (3333) balance to $14.99, and will for every email like this");
  expect(await balance()).toBe(-1499);
  await page.keyboard.press("Escape");
  await expect(page).not.toHaveURL(/email=/);

  // The next summary goes through the new filter, without AI.
  await req.post(`${fakeImap}/deliver`, {
    data: email("bal-2", "Your requested balance summary", `Quicksilver (3333) has a balance of $20.00 as of ${today}.`),
  });
  await expect
    .poll(
      async () => {
        await checkMail();
        return balance();
      },
      { timeout: 20_000 },
    )
    .toBe(-2000);
  await page.goto("/settings#email-filters");
  await expect(page.getByTestId("email-filter-row").filter({ hasText: "updates the balance" })).toBeVisible();

  // Ignore emails like the security alert from the AI spec.
  await page.getByRole("button", { name: /^Notifications/ }).first().click();
  await page.getByRole("button", { name: /From your bank/ }).click();
  await page.getByTestId("notification-list").getByRole("button", { name: /Security alert · Quicksilver/ }).click();
  const secActions = page.getByRole("dialog").getByTestId("notice-actions");
  await secActions.getByRole("button", { name: "Ignore emails like this" }).click();
  await expect(secActions.getByLabel("Subject contains")).toHaveValue("Unusual activity on your");
  await secActions.getByLabel("Subject contains").fill("Unusual activity");
  await secActions.getByRole("button", { name: "Ignore", exact: true }).click();
  await expect(page.getByRole("dialog").getByTestId("notice-applied")).toContainText("Ignoring emails like this");
  await page.keyboard.press("Escape");
  await page.goto("/settings#email-filters");
  await expect(page.getByTestId("email-filter-row").filter({ hasText: "→ ignored" })).toBeVisible();
});
