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

async function makeLink(page: Page, open: () => Promise<void>) {
  await open();
  const dlg = page.getByRole("dialog");
  await dlg.getByRole("button", { name: "Create link" }).click();
  const link = (await dlg.getByTestId("invite-link").textContent())!.trim();
  await dlg.getByRole("button", { name: "Done" }).click();
  return link;
}

test("invite a member, share data, owners, reset link and removal", async ({ page, browser }) => {
  await login(page);
  await page.goto("/settings?tab=household");
  const members = page.getByTestId("member-row");
  await expect(members).toHaveCount(1);
  await expect(members.first()).toContainText("Admin");

  const invite = await makeLink(page, async () => {
    await page.getByRole("button", { name: "Invite someone" }).click();
    await page.getByRole("dialog").getByLabel("Who's it for?").fill("Sam");
  });
  expect(invite).toMatch(/\/join\/[\w-]+$/);
  await expect(page.getByTestId("invite-row")).toContainText("Invite for Sam");
  await page.screenshot({ path: `${shots}/21-household.png` });

  // Sam joins from another browser and lands in the shared household.
  const samCtx = await browser.newContext();
  const sam = await samCtx.newPage();
  await sam.goto(invite);
  await expect(sam.getByRole("heading", { name: /^Join / })).toBeVisible();
  await sam.getByLabel("Your name").fill("Sam Rivera");
  await sam.getByLabel("Email").fill("sam@example.com");
  await sam.getByLabel("Password", { exact: true }).fill("sam's long password");
  await sam.getByLabel("Confirm password").fill("sam's long password");
  await sam.getByRole("button", { name: "Join household" }).click();
  await expect(sam.getByRole("heading", { name: "Dashboard", level: 1 })).toBeVisible();
  await sam.goto("/settings?tab=household");
  await expect(sam.getByTestId("member-row")).toHaveCount(2);
  await expect(sam.getByText("Only an admin can invite or remove members.")).toBeVisible();
  await expect(sam.getByRole("button", { name: "Invite someone" })).toHaveCount(0);
  // The used link is gone and can't be used again.
  await page.reload();
  await expect(members).toHaveCount(2);
  await expect(page.getByTestId("invite-row")).toHaveCount(0);
  const again = await browser.newPage();
  await again.goto(invite);
  await expect(again.getByRole("heading", { name: "This link doesn't work" })).toBeVisible();
  await again.close();

  // Roles.
  const samRow = members.filter({ hasText: "Sam Rivera" });
  await page.getByRole("button", { name: "Manage Sam Rivera" }).click();
  await page.getByRole("menuitem", { name: "Make admin" }).click();
  await expect(samRow).toContainText("Admin");
  await page.getByRole("button", { name: "Manage Sam Rivera" }).click();
  await page.getByRole("menuitem", { name: "Remove admin" }).click();
  await expect(samRow).not.toContainText("Admin");

  // Owners: Sam owns a manual account; its transactions follow, with a filter.
  const acct = await (await page.request.post("/api/accounts", { headers: csrf, data: { name: "Sam's Wallet", type: "cash", balance: "40" } })).json();
  await page.request.post("/api/transactions", { headers: csrf, data: { account_id: acct.id, date: new Date().toISOString().slice(0, 10), amount: "-6.50", description: "Sam Bakery" } });
  await page.goto("/accounts");
  await page.getByTestId("account-row").filter({ hasText: "Sam's Wallet" }).click();
  await page.getByRole("dialog").getByLabel("Owner").selectOption({ label: "Sam Rivera" });
  await page.keyboard.press("Escape");
  await expect(page.getByTestId("account-row").filter({ hasText: "Sam's Wallet" })).toContainText("Sam");

  await page.goto("/transactions");
  await page.getByLabel("Search transactions").fill("Sam Bakery");
  const row = page.getByTestId("txn-row").filter({ hasText: "Sam Bakery" });
  // Added by the admin, so the entry itself belongs to them until set back to the account's owner.
  await page.getByLabel("Owner").selectOption({ label: "Sam" });
  await expect(row).toHaveCount(0);
  await page.getByLabel("Owner").selectOption({ label: "Everyone" });
  await row.click();
  const owner = page.getByRole("dialog").getByLabel("Owner");
  await expect(owner).not.toHaveValue("account");
  await owner.selectOption({ label: "Same as account (Sam)" });
  await expect(owner).toHaveValue("account");
  await page.keyboard.press("Escape");
  await page.getByLabel("Owner").selectOption({ label: "Sam" });
  await expect(row).toBeVisible();
  await page.getByLabel("Owner").selectOption({ label: "Shared" });
  await expect(row).toHaveCount(0);

  // A password reset link signs Sam in with the new password.
  await page.goto("/settings?tab=household");
  const reset = await makeLink(page, async () => {
    await page.getByRole("button", { name: "Manage Sam Rivera" }).click();
    await page.getByRole("menuitem", { name: "Password reset link" }).click();
  });
  await expect(page.getByTestId("invite-row")).toContainText("Password reset for Sam Rivera");
  await samCtx.clearCookies();
  await sam.goto(reset);
  await expect(sam.getByRole("heading", { name: "Choose a new password" })).toBeVisible();
  await sam.getByLabel("New password").fill("sam's newer password");
  await sam.getByLabel("Confirm password").fill("sam's newer password");
  await sam.getByRole("button", { name: "Set password" }).click();
  await expect(sam.getByRole("heading", { name: "Dashboard", level: 1 })).toBeVisible();

  // Removing Sam ends their access; the account stays, shared.
  await page.reload();
  await page.getByRole("button", { name: "Manage Sam Rivera" }).click();
  await page.getByRole("menuitem", { name: "Remove from household" }).click();
  await page.getByRole("dialog").getByRole("button", { name: "Remove" }).click();
  await expect(members).toHaveCount(1);
  await sam.goto("/accounts");
  await expect(sam).toHaveURL(/\/login/);
  await samCtx.close();
  const accts = await (await page.request.get("/api/accounts")).json();
  expect(accts.accounts.find((a: { id: number }) => a.id === acct.id).owner_id).toBeNull();
});

test("settings tabs and the rules page", async ({ page }) => {
  await login(page);
  await page.getByRole("button", { name: /^Notifications/ }).first().click();
  await page.getByRole("button", { name: "Settings", exact: true }).click();
  await expect(page).toHaveURL(/tab=notifications/);
  await expect(page.getByText("Alert me when")).toBeVisible();
  await page.getByRole("link", { name: "Rules" }).first().click();
  await expect(page.getByRole("heading", { name: "Rules", level: 1 })).toBeVisible();
  await expect(page.getByTestId("rule-row").first()).toBeVisible();
});

test("recurring calendar sits under the paycheck card", async ({ page }) => {
  await login(page);
  await page.goto("/recurring");
  const headings = page.locator("main section h2");
  await expect(headings.first()).toContainText("Before your next paycheck");
  const month = new Date().toLocaleDateString("en-US", { month: "long", year: "numeric" });
  await expect(headings.nth(1)).toHaveText(month);
});
