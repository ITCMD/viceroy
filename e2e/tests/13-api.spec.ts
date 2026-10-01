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

test("REST API: enable, generate a key, call the API, read the docs, revoke", async ({ page, playwright, baseURL }) => {
  await login(page);
  await page.goto("/settings#api");
  const card = page.locator("section", { has: page.getByRole("heading", { name: "API", exact: true }) });
  const toggle = card.getByRole("switch", { name: /Enable the REST API/ });
  await expect(toggle).not.toBeChecked();

  await card.getByRole("button", { name: "Generate key" }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByLabel("Name").fill("Spreadsheet");
  await dialog.getByRole("button", { name: "Generate" }).click();
  const key = (await dialog.getByTestId("new-api-key").textContent())!.trim();
  expect(key).toMatch(/^vk_[A-Za-z0-9]{40}$/);
  await expect(dialog).toContainText("Turn on the REST API");
  await page.screenshot({ path: `${shots}/13-new-key.png` });
  await dialog.getByRole("button", { name: "Done" }).click();
  await expect(card.getByTestId("api-key-row")).toContainText("Spreadsheet");

  // A separate client with only the key (no cookies).
  const client = await playwright.request.newContext({ baseURL, extraHTTPHeaders: { Authorization: `Bearer ${key}` } });
  expect((await client.get("/api/accounts")).status()).toBe(403); // API still off
  await toggle.click();
  await expect(toggle).toBeChecked();
  const res = await client.get("/api/accounts");
  expect(res.status()).toBe(200);
  expect((await res.json()).accounts.length).toBeGreaterThan(0);
  expect((await client.post("/api/goals", { data: { name: "Nope" } })).status()).toBe(403); // read-only
  await page.reload();
  await expect(card.getByTestId("api-key-row")).toContainText("used just now");

  // Docs.
  await card.getByRole("link", { name: "API documentation" }).click();
  await expect(page.getByRole("heading", { name: "API documentation", level: 1 })).toBeVisible();
  await expect(page.getByText("/api/transactions/{id}").first()).toBeVisible();
  await expect(page.getByTestId("api-doc-group").first()).toBeVisible();
  await page.screenshot({ path: `${shots}/13-api-docs.png` });

  // Revoke.
  await page.goto("/settings#api");
  await card.getByRole("button", { name: "Revoke Spreadsheet" }).click();
  await expect(card.getByTestId("api-key-row")).toHaveCount(0);
  expect((await client.get("/api/accounts")).status()).toBe(401);
  await client.dispose();
});
