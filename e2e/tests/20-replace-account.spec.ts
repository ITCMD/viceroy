import { expect, test, type Page } from "@playwright/test";

// Runs last: it changes what the fake bank reports.
const shots = process.env.SCREENSHOT_DIR ?? "test-results/screens";
const admin = { email: "admin@example.com", password: "correct horse battery" };
const fake = "http://127.0.0.1:28430";

async function login(page: Page) {
  await page.goto("/login");
  await page.getByLabel("Email").fill(admin.email);
  await page.getByLabel("Password").fill(admin.password);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page.getByRole("heading", { name: "Dashboard", level: 1 })).toBeVisible();
}

test("replace an account with its sync duplicate", async ({ page, request }) => {
  await login(page);
  // The card now also comes through a second login, under a new id and name.
  await request.post(`${fake}/_control/scenario`, { data: { name: "relinked+dupcard" } });
  await page.goto("/accounts");
  await page.getByRole("button", { name: "Sync now" }).click();
  const banner = page.getByRole("button", { name: /1 new account on SimpleFIN: Quicksilver Rewards/ });
  await expect(banner).toBeVisible();

  // The account picker spots the twin.
  await banner.click();
  const hint = page.getByTestId("twin-hint");
  await expect(hint).toContainText("Same last 4 digits as Quicksilver Card (3333)");
  await expect(hint).toContainText("If this account is jointly owned and both logins have access to it, simply leave this copy disabled.");
  await expect(page.getByTestId("offer-hint").getByRole("button", { name: "Dismiss" })).toBeVisible();
  await page.screenshot({ path: `${shots}/20-replace-hint.png` });
  await page.keyboard.press("Escape");

  // Replace from the old account's settings.
  const accts = (await (await page.request.get("/api/accounts")).json()).accounts as { id: number; name: string }[];
  const oldID = accts.find((a) => a.name === "Quicksilver Card (3333)")!.id;
  const before = (await (await page.request.get(`/api/transactions?account=${oldID}`)).json()).transactions.length;
  await page.getByTestId("account-row").filter({ hasText: "Quicksilver Card (3333)" }).click();
  const sheet = page.getByRole("dialog");
  const section = sheet.getByTestId("replace-account");
  await section.getByLabel("Replace account").selectOption({ label: "Quicksilver Rewards (3333) (Capital One · ••3333, new on SimpleFIN)" });
  await section.scrollIntoViewIfNeeded();
  await page.screenshot({ path: `${shots}/20-replace-sheet.png` });
  page.once("dialog", (d) => d.accept());
  await section.getByRole("button", { name: "Replace" }).click();
  await expect(sheet).toBeHidden();

  // One account, keeping its name and transactions, now fed by the new link.
  await expect(page.getByTestId("account-row").filter({ hasText: "Quicksilver Card (3333)" })).toHaveCount(1);
  await expect(banner).toBeHidden();
  const after = (await (await page.request.get("/api/accounts")).json()).accounts as { id: number; name: string; provider_name: string; replaced_by: number | null }[];
  const repl = after.find((a) => a.name === "Quicksilver Card (3333)" && !a.replaced_by)!;
  expect(repl.provider_name).toBe("Quicksilver Rewards (3333)");
  expect(after.find((a) => a.id === oldID)!.replaced_by).toBe(repl.id);
  const moved = (await (await page.request.get(`/api/transactions?account=${repl.id}`)).json()).transactions.length;
  expect(moved).toBe(before);

  // Dismissing an offered account clears the New flag and the banner.
  await request.post(`${fake}/_control/scenario`, { data: { name: "relinked+dupcard+newbank" } });
  await page.getByRole("button", { name: "Sync now" }).click();
  const offer = page.getByRole("button", { name: /1 new account on SimpleFIN: Sapphire Preferred/ });
  await offer.click();
  const row = page.getByTestId("manage-row").filter({ hasText: "Sapphire Preferred" });
  await row.getByRole("button", { name: "Dismiss" }).click();
  await expect(row.getByText("New")).toHaveCount(0);
  await expect(row.getByRole("switch")).not.toBeChecked();
  await page.keyboard.press("Escape");
  await expect(offer).toBeHidden();

  // Another sync doesn't bring the old link back (or the dismissed account).
  await page.getByRole("button", { name: "Sync now" }).click();
  await expect(page.getByRole("button", { name: "Sync now" })).toBeEnabled();
  await expect(page.getByRole("button", { name: /new account on SimpleFIN/ })).toHaveCount(0);
  await expect(page.getByTestId("account-row").filter({ hasText: "Quicksilver Card (3333)" })).toHaveCount(1);
});
