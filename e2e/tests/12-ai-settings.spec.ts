import { expect, test, type Page } from "@playwright/test";

const shots = process.env.SCREENSHOT_DIR ?? "test-results/screens";
const admin = { email: "admin@example.com", password: "correct horse battery" };

async function login(page: Page) {
  await page.goto("/login");
  await page.getByLabel("Email").fill(admin.email);
  await page.getByLabel("Password").fill(admin.password);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page.getByRole("heading", { name: "Dashboard", level: 1 })).toBeVisible();
}

test("AI settings: save a key, pick models, test, remove", async ({ page }) => {
  await login(page);
  await page.goto("/settings#ai");
  const card = page.locator("section", { has: page.getByRole("heading", { name: "AI", exact: true }) });
  await expect(card.getByTestId("ai-key-status")).toContainText("(from viceroy.toml)");

  // Replace the config key with one saved in Settings (the fake accepts "test-key").
  await card.getByRole("button", { name: "Replace" }).click();
  await card.getByLabel("OpenRouter API key").fill("test-key");
  await card.getByRole("button", { name: "Save key" }).click();
  await expect(card.getByTestId("ai-key-status")).toHaveText("Saved key ending in -key");

  await card.getByLabel("Email reading model").fill("cheap/flash");
  await card.getByRole("button", { name: "Save", exact: true }).click();
  await expect(card).toContainText("cheap/flash via OpenRouter");
  await card.getByRole("button", { name: "Test email reading" }).click();
  await expect(card.getByRole("status")).toHaveText("Works (cheap/flash).");
  await card.screenshot({ path: `${shots}/12-ai-settings.png` });

  // Removing the saved key falls back to viceroy.toml's.
  await card.getByRole("button", { name: "Remove" }).click();
  await expect(card.getByTestId("ai-key-status")).toContainText("(from viceroy.toml)");
  await card.getByLabel("Email reading model").fill("");
  await card.getByRole("button", { name: "Save", exact: true }).click();
  await expect(card.getByRole("button", { name: "Save", exact: true })).toHaveCount(0);
});
