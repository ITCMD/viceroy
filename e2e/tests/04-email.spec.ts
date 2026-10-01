import { expect, test, type Page } from "@playwright/test";

const shots = process.env.SCREENSHOT_DIR ?? "test-results/screens";
const shot = (page: Page, name: string) => page.screenshot({ path: `${shots}/${name}.png`, fullPage: true });
const fakeImap = "http://127.0.0.1:28432";
const admin = { email: "admin@example.com", password: "correct horse battery" };

test.describe.configure({ mode: "serial" });

async function login(page: Page) {
  await page.goto("/login");
  await page.getByLabel("Email").fill(admin.email);
  await page.getByLabel("Password").fill(admin.password);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page.getByRole("heading", { name: "Dashboard", level: 1 })).toBeVisible();
}

async function openSettings(page: Page) {
  await page.getByRole("link", { name: "Settings" }).first().click();
  await expect(page.getByRole("heading", { name: "Settings", level: 1 })).toBeVisible();
}

async function chooseAccount(page: Page, dialog: ReturnType<Page["getByRole"]>, name: string) {
  const select = dialog.getByLabel("Account", { exact: true });
  const label = await select.locator("option", { hasText: name }).first().textContent();
  await select.selectOption({ label: label! });
}

const reviewRow = (page: Page, subject: string) => page.getByTestId("review-email-row").filter({ hasText: subject });

test("connect a mailbox and list emails to review", async ({ page }) => {
  await login(page);
  await openSettings(page);
  await expect(page.getByText("Get transactions in minutes")).toBeVisible();
  await page.getByRole("button", { name: "Connect mailbox" }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByLabel("Provider").selectOption({ label: "Other (IMAP)" });
  await dialog.getByLabel("Email address or username").fill("test@example.com");
  await dialog.getByLabel("App password").fill("wrong");
  await dialog.getByLabel("IMAP server").fill("127.0.0.1");
  await dialog.getByLabel("Port").fill("28431");
  await dialog.getByLabel("Security", { exact: true }).selectOption("none");
  await dialog.getByLabel("Folder or label").fill("INBOX");
  await dialog.getByRole("button", { name: "Connect" }).click();
  await expect(dialog.getByText(/Couldn't connect: login failed/)).toBeVisible();
  await dialog.getByLabel("App password").fill("app-pass");
  await dialog.getByRole("button", { name: "Connect" }).click();
  await expect(dialog).toBeHidden();

  await expect(page.getByTestId("mailbox-row").filter({ hasText: "test@example.com" })).toContainText("Watching");
  await expect(page.getByTestId("review-email-row")).toHaveCount(4);
  await expect(reviewRow(page, "Our fall sale starts now")).toContainText("No filter");
  await shot(page, "email-settings");

  // The Transactions page points at the waiting emails.
  await page.getByRole("link", { name: "Transactions" }).first().click();
  await expect(page.getByTestId("email-review-banner")).toContainText("4 emails need a filter");
  await page.getByTestId("email-review-banner").click();
  await expect(page.getByRole("heading", { name: "Settings", level: 1 })).toBeVisible();

  // Viewing an email shows its text, never raw HTML.
  await reviewRow(page, "Your $12.34 transaction").getByRole("button").first().click();
  const viewer = page.getByRole("dialog");
  await expect(viewer).toContainText("Merchant\nSTARBUCKS STORE 123");
  await expect(viewer).not.toContainText("<td>");
  await viewer.getByRole("button", { name: "Close" }).click();
});

test("custom parser filter turns an email into a final transaction", async ({ page }) => {
  await login(page);
  await openSettings(page);
  await reviewRow(page, "Debit card purchase alert").getByRole("button", { name: "Create filter" }).click();
  const dialog = page.getByRole("dialog");
  await expect(dialog.getByLabel("From")).toHaveValue("mycu.org");
  await expect(dialog.getByLabel("Subject contains")).toHaveValue("Debit card purchase alert");
  // The generic parser already reads the sample...
  await expect(dialog.getByTestId("sample-result")).toContainText("SHELL OIL 57442");
  // ...but build a custom one against it.
  await dialog.getByLabel("Parser").selectOption("custom");
  await dialog.getByLabel("Amount: text before").fill("Amount:");
  await dialog.getByLabel("Merchant: text before").fill("Merchant:");
  await dialog.getByLabel("Merchant: text after").fill("5744");
  await expect(dialog.getByTestId("sample-result")).toContainText("$48.10");
  await expect(dialog.getByTestId("sample-result")).toContainText("SHELL OIL");
  await expect(dialog.getByTestId("sample-result")).not.toContainText("57442");
  await chooseAccount(page, dialog, "Wallet");
  await expect(dialog.getByText("Email-only account")).toBeVisible();
  await shot(page, "email-filter-dialog");
  await dialog.getByRole("button", { name: "Save filter" }).click();
  await expect(dialog.getByTestId("filter-routed")).toContainText("1 waiting email was added");
  await dialog.getByRole("button", { name: "Done" }).click();
  await expect(page.getByTestId("email-filter-row").filter({ hasText: "mycu.org" })).toContainText("custom parser");
  await expect(reviewRow(page, "Debit card purchase alert")).toHaveCount(0);

  await page.getByRole("link", { name: "Transactions" }).first().click();
  const row = page.getByTestId("txn-row").filter({ hasText: "Shell Oil" });
  await expect(row).toContainText("-$48.10");
  await expect(row).not.toContainText("Email alert"); // final on an email-only account
});

test("template filter on a synced account creates a pending email alert", async ({ page, request }) => {
  await login(page);
  await openSettings(page);
  await reviewRow(page, "A new transaction was charged").getByRole("button", { name: "Create filter" }).click();
  const dialog = page.getByRole("dialog");
  await expect(dialog.getByLabel("From")).toHaveValue("notification.capitalone.com");
  await dialog.getByLabel("Parser").selectOption("capital_one");
  await expect(dialog.getByTestId("sample-result")).toContainText("TRADER JOE'S #552");
  await chooseAccount(page, dialog, "Quicksilver");
  await expect(dialog.getByText("Synced account")).toBeVisible();
  await dialog.getByRole("button", { name: "Save filter" }).click();
  await dialog.getByRole("button", { name: "Done" }).click();

  await reviewRow(page, "Our fall sale starts now").getByRole("button", { name: "Ignore" }).click();
  await expect(page.getByTestId("review-email-row")).toHaveCount(1);

  await page.getByRole("link", { name: "Transactions" }).first().click();
  await expect(page.getByTestId("email-review-banner")).toContainText("1 email needs a filter");
  const row = page.getByTestId("txn-row").filter({ hasText: "Email alert" });
  await expect(row).toContainText("-$1,045.20");
  await row.click();
  const sheet = page.getByRole("dialog");
  await sheet.getByTestId("txn-email").click();
  await expect(page.getByRole("dialog").getByText(/in the amount of \$1,045.20/)).toBeVisible();
  await shot(page, "email-txn-source");
  await page.keyboard.press("Escape");

  // A new alert arriving while the watcher idles shows up without a manual check.
  const raw = [
    "From: Credit Union Alerts <alerts@mycu.org>",
    "Subject: Debit card purchase alert",
    `Date: ${new Date().toUTCString()}`,
    "Message-ID: <cu-live@mycu.org>",
    "Content-Type: text/plain",
    "",
    "Merchant: CORNER BAKERY 57441",
    "Amount: $6.75",
    "",
  ].join("\r\n");
  await request.post(`${fakeImap}/deliver`, { data: raw });
  await expect(async () => {
    await page.reload();
    await expect(page.getByTestId("txn-row").filter({ hasText: "Corner Bakery" })).toContainText("-$6.75", { timeout: 1000 });
  }).toPass({ timeout: 15_000 });
});
