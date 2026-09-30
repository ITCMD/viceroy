import { expect, test, type Page } from "@playwright/test";

const shots = process.env.SCREENSHOT_DIR ?? "test-results/screens";
const shot = (page: Page, name: string) => page.screenshot({ path: `${shots}/${name}.png`, fullPage: true });
const admin = { email: "admin@example.com", password: "correct horse battery" };

async function login(page: Page) {
  await page.goto("/login");
  await page.getByLabel("Email").fill(admin.email);
  await page.getByLabel("Password").fill(admin.password);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page.getByRole("heading", { name: "Dashboard", level: 1 })).toBeVisible();
}

const iso = (d: Date) => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
const daysAgo = (n: number) => {
  const d = new Date();
  d.setDate(d.getDate() - n);
  return iso(d);
};

const transactions = [
  "Date,Merchant,Category,Account,Original Statement,Notes,Amount,Tags,Owner,Reviewed,Id",
  `${daysAgo(200)},Corner Bakery,Restaurants & Bars,Imported Checking (...9191),CORNER BAKERY,,-14.25,,Shared,,m1`,
  `${daysAgo(150)},Self Storage Co,Storage Unit,Imported Checking (...9191),SELF STORAGE,,-89.00,Monthly,Shared,,m2`,
  `${daysAgo(120)},Payroll,Paychecks,Imported Checking (...9191),ACME PAYROLL,"bonus, yay",2500.00,,Shared,,m3`,
].join("\n");
const balances = ["Date,Balance,Account", `${daysAgo(30)},-8000.00,Family Car (...9292)`, `${daysAgo(0)},-7600.00,Family Car (...9292)`].join("\n");

test("import Monarch exports", async ({ page }) => {
  await login(page);
  await page.goto("/settings");
  await page.getByRole("button", { name: "Import from Monarch" }).click();
  const dialog = page.getByRole("dialog", { name: "Import from Monarch" });
  await dialog.getByLabel("Monarch CSV files").setInputFiles([
    { name: "Transactions_2026.csv", mimeType: "text/csv", buffer: Buffer.from(transactions) },
    { name: "Balances_2026.csv", mimeType: "text/csv", buffer: Buffer.from(balances) },
  ]);
  await expect(dialog.getByText("Transactions_2026.csv")).toBeVisible();
  await expect(dialog.getByText("Balances_2026.csv")).toBeVisible();
  await dialog.getByRole("button", { name: "Continue" }).click();

  await expect(dialog.getByText("3 transactions", { exact: true })).toBeVisible();
  await expect(dialog.getByTestId("import-account")).toHaveCount(2);
  // The car has no transactions and a negative balance: suggested as a loan.
  await expect(dialog.getByLabel("Type for Family Car (...9292)")).toHaveValue("loan");
  await dialog.getByLabel("Name for Imported Checking (...9191)").fill("Old Checking");
  // Only the category Viceroy doesn't have is listed; it defaults to a new category.
  await expect(dialog.getByTestId("import-category")).toHaveCount(1);
  await expect(dialog.getByLabel("Category for Storage Unit")).toHaveValue(/^new:/);
  await shot(page, "08-import-review");
  await dialog.getByRole("button", { name: "Import 3 transactions" }).click();

  const done = dialog.getByRole("status");
  await expect(done).toContainText("3 transactions imported");
  await expect(done).toContainText("2 accounts created");
  await expect(done).toContainText("1 categories created");
  await dialog.getByRole("button", { name: "Done" }).click();

  await page.goto("/transactions");
  await page.getByText("Self Storage Co").first().click();
  await expect(page.getByText("Imported", { exact: true })).toBeVisible();
  await expect(page.getByText("Storage Unit").first()).toBeVisible();

  // Net worth history reaches back to the oldest imported transaction.
  const hist = await (await page.request.get("/api/networth/history?days=365")).json();
  expect(hist.points[0].date <= daysAgo(200)).toBeTruthy();

  // A second import of the same files skips everything.
  await page.goto("/settings");
  await page.getByRole("button", { name: "Import from Monarch" }).click();
  await dialog.getByLabel("Monarch CSV files").setInputFiles([{ name: "Transactions_2026.csv", mimeType: "text/csv", buffer: Buffer.from(transactions) }]);
  await dialog.getByRole("button", { name: "Continue" }).click();
  await expect(dialog.getByText("3 were imported before and will be skipped.")).toBeVisible();
});
