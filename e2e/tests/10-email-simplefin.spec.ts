import { expect, test, type Page } from "@playwright/test";

const shots = process.env.SCREENSHOT_DIR ?? "test-results/screens";
const fakeBank = "http://127.0.0.1:28430";
const fakeImap = "http://127.0.0.1:28432";
const admin = { email: "admin@example.com", password: "correct horse battery" };
const headers = { "X-Viceroy-CSRF": "1" };

async function login(page: Page) {
  await page.goto("/login");
  await page.getByLabel("Email").fill(admin.email);
  await page.getByLabel("Password").fill(admin.password);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page.getByRole("heading", { name: "Dashboard", level: 1 })).toBeVisible();
}

test("email alert shows first, SimpleFIN confirms it without a duplicate (with a tip)", async ({ page }) => {
  await login(page);
  const req = page.request;
  const { accounts } = await (await req.get("/api/accounts")).json();
  const checking = accounts.find((a: { name: string }) => a.name.startsWith("360 Checking"));
  expect(checking).toBeTruthy();
  const filter = await req.post("/api/email/filters", {
    headers,
    data: { name: "Debit card alerts", sender: "debit.example.com", account_id: checking.id, parser: "generic" },
  });
  expect(filter.ok()).toBeTruthy();

  const today = new Date().toLocaleDateString("en-US", { month: "long", day: "numeric", year: "numeric" });
  const raw = [
    "From: Debit Alerts <alerts@debit.example.com>",
    "Subject: Debit card purchase",
    `Date: ${new Date().toUTCString()}`,
    "Message-ID: <dinner-1@debit.example.com>",
    "Content-Type: text/plain",
    "",
    `You made a purchase of $50.00 at LUNA TRATTORIA on ${today}.`,
    "",
  ].join("\r\n");
  await req.post(`${fakeImap}/deliver`, { data: raw });

  // Shows up right away as a pending email alert.
  await page.goto("/transactions");
  const rows = page.getByTestId("txn-row").filter({ hasText: /Luna Trattoria/i });
  await expect(rows).toHaveCount(1, { timeout: 15_000 });
  await expect(rows.getByLabel("From an email alert")).toBeVisible();
  await expect(rows).toContainText("-$50.00");
  await expect(rows).toContainText("Email alert");

  // The bank posts it for $60.00 (tip included) on the next sync.
  await req.post(`${fakeBank}/_control/scenario`, { data: { name: "relinked+posted+dinner" } });
  const { connections } = await (await req.get("/api/connections")).json();
  for (const c of connections) await req.post(`/api/connections/${c.id}/sync`, { headers });

  await page.reload();
  await expect(rows).toHaveCount(1);
  await expect(rows).toContainText("-$60.00");
  await expect(rows.getByLabel("From an email alert")).toHaveCount(0);
  await expect(rows.getByText("Email alert")).toHaveCount(0);

  await rows.click();
  const sources = page.getByRole("dialog").getByTestId("txn-sources");
  await expect(sources).toContainText("SimpleFIN");
  await expect(sources).toContainText("Email alert");
  await expect(page.getByRole("dialog").getByTestId("txn-email")).toContainText("Debit card purchase");
  await page.screenshot({ path: `${shots}/10-confirmed.png` });
});
