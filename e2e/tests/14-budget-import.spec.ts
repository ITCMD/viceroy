import { expect, test, type Page } from "@playwright/test";
import { readFile } from "node:fs/promises";

const shots = process.env.SCREENSHOT_DIR ?? "test-results/screens";
const admin = { email: "admin@example.com", password: "correct horse battery" };
const png = Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==", "base64");

async function login(page: Page) {
  await page.goto("/login");
  await page.getByLabel("Email").fill(admin.email);
  await page.getByLabel("Password").fill(admin.password);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page.getByRole("heading", { name: "Dashboard", level: 1 })).toBeVisible();
}

const row = (page: Page, name: string) => page.getByTestId("import-row").filter({ has: page.getByText(name, { exact: true }) });

test("export the budget, import it back from CSV, text and a screenshot", async ({ page }) => {
  await login(page);
  await page.goto("/budget");
  await expect(page.getByTestId("budget-line").first()).toBeVisible();

  // Export: a CSV of every budget line.
  const [download] = await Promise.all([page.waitForEvent("download"), page.getByRole("link", { name: "Export budget as CSV" }).click()]);
  expect(download.suggestedFilename()).toMatch(/^viceroy-budget-\d{4}-\d{2}\.csv$/);
  const exported = await readFile(await download.path(), "utf8");
  expect(exported.split("\n")[0]).toBe("Group,Category,Amount,Timing,Icon");
  expect(exported).toContain(",Groceries,");

  // CSV: one new category, one change, one bad line reported.
  await page.getByRole("button", { name: "Import" }).click();
  const dialog = page.getByRole("dialog", { name: "Import budget" });
  const csv = "Group,Category,Amount,Timing\nFlexible,Pottery Class,55,Week 2\nFlexible,Coffee Shops,42.50,\nFlexible,Oops,abc,\n";
  await dialog.getByLabel("Budget CSV file").setInputFiles({ name: "budget.csv", mimeType: "text/csv", buffer: Buffer.from(csv) });
  await dialog.getByRole("button", { name: "Continue" }).click();
  await expect(dialog.getByTestId("import-row")).toHaveCount(2);
  await expect(dialog.getByTestId("import-problems")).toContainText("Oops");
  await expect(row(page, "Pottery Class")).toContainText("New");
  await expect(row(page, "Pottery Class")).toContainText("In week 2");
  await row(page, "Pottery Class").getByLabel("Monthly amount for Pottery Class").fill("60");
  await dialog.screenshot({ path: `${shots}/14-budget-import-review.png` });
  await dialog.getByRole("button", { name: /Apply 2 changes/ }).click();
  await expect(dialog.getByRole("status")).toContainText("Budget updated");
  await dialog.getByRole("button", { name: "Done" }).click();
  const pottery = page.getByTestId("budget-line").filter({ hasText: "Pottery Class" });
  await expect(pottery).toContainText("$60");

  // Pasted text, read by the AI: "Dining out" maps onto Restaurants & Bars.
  await page.getByRole("button", { name: "Import" }).click();
  await dialog.getByRole("tab", { name: "Paste text" }).click();
  await dialog.getByLabel("Your budget").fill("Dining out: $310\nPottery Class 60\nTotal 370");
  await dialog.getByRole("button", { name: "Read with AI" }).click();
  await expect(row(page, "Restaurants & Bars")).toBeVisible();
  await expect(row(page, "Pottery Class")).toContainText("No change");
  await expect(dialog.getByRole("button", { name: "Apply 1 change" })).toBeEnabled();
  // Re-point a row and skip another.
  await row(page, "Pottery Class").getByLabel("Import Pottery Class as").selectOption({ label: "Don't import" });
  await expect(row(page, "Pottery Class")).toContainText("Skipped");
  await dialog.getByRole("button", { name: "Apply 1 change" }).click();
  await expect(dialog.getByRole("status")).toContainText("1 budget lines changed");
  await dialog.getByRole("button", { name: "Done" }).click();

  // A screenshot, read by the multimodal model.
  await page.getByRole("button", { name: "Import" }).click();
  await dialog.getByRole("tab", { name: "Screenshot" }).click();
  await dialog.getByLabel("Budget screenshots").setInputFiles({ name: "budget.png", mimeType: "image/png", buffer: png });
  await expect(dialog.getByRole("img", { name: "Screenshot 1" })).toBeVisible();
  await dialog.getByRole("button", { name: "Read with AI" }).click();
  await expect(dialog.getByTestId("import-row")).toHaveCount(4);
  await expect(dialog.getByTestId("import-problems")).toContainText("Read from the screenshot");
  await expect(row(page, "Hobbies")).toContainText("New");
  await dialog.screenshot({ path: `${shots}/14-budget-import-ai.png` });
  await dialog.getByRole("button", { name: "Back" }).click();
  await dialog.getByRole("button", { name: "Cancel" }).click();
});

test("multimodal model in Settings → AI", async ({ page }) => {
  await login(page);
  await page.goto("/settings#ai");
  const card = page.locator("section", { has: page.getByRole("heading", { name: "AI", exact: true }) });
  // The picker lists OpenRouter's models, image-capable ones by default.
  await card.getByLabel("Multimodal model").click();
  const list = page.getByRole("listbox", { name: "Multimodal model" });
  await expect(list.getByRole("option", { name: /Claude Sonnet 5\.5/ })).toContainText("$3 / $15");
  await expect(list.getByRole("option", { name: /DeepSeek/ })).toHaveCount(0);
  await page.screenshot({ path: `${shots}/14-model-picker.png` });
  await page.getByLabel("Only models that accept images").uncheck();
  await expect(list.getByRole("option", { name: /DeepSeek/ })).toBeVisible();
  await page.getByLabel("Search models").fill("gemini");
  await list.getByRole("option", { name: /Gemini 3 Pro/ }).click();
  await expect(card.getByLabel("Multimodal model")).toHaveText("Google: Gemini 3 Pro");
  await card.getByRole("button", { name: "Save", exact: true }).click();
  await expect(card).toContainText("google/gemini-3-pro");
  await card.getByRole("button", { name: "Test images" }).click();
  await expect(card.getByRole("status")).toHaveText("Works (google/gemini-3-pro).");
  await card.getByLabel("Multimodal model").click();
  await page.getByRole("option", { name: "Same as chat model" }).click();
  await card.getByRole("button", { name: "Save", exact: true }).click();
  await expect(card.getByRole("button", { name: "Save", exact: true })).toHaveCount(0);
});
