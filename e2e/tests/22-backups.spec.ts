import { expect, test } from "@playwright/test";

const shots = process.env.SCREENSHOT_DIR ?? "test-results/screens";
const admin = { email: "admin@example.com", password: "correct horse battery" };

test("backups card: daily backup exists and Back up now adds one", async ({ page }) => {
  await page.goto("/login");
  await page.getByLabel("Email").fill(admin.email);
  await page.getByLabel("Password").fill(admin.password);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page.getByRole("heading", { name: "Dashboard", level: 1 })).toBeVisible();

  await page.goto("/settings#backups");
  const card = page.locator("#backups");
  await expect(card).toContainText("the last 14 are kept");
  const rows = card.getByTestId("backup-list").locator("li");
  // The server made one when it started.
  await expect(rows).toHaveCount(1);
  await card.getByRole("button", { name: "Back up now" }).click();
  await expect(rows).toHaveCount(2);
  await expect(rows.first()).toContainText("just now");
  await card.scrollIntoViewIfNeeded();
  await page.screenshot({ path: `${shots}/22-backups.png` });
});
