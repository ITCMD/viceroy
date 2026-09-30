import { expect, test, type Page } from "@playwright/test";

// Screenshots land here for visual review; override with SCREENSHOT_DIR.
const shots = process.env.SCREENSHOT_DIR ?? "test-results/screens";
const shot = (page: Page, name: string) => page.screenshot({ path: `${shots}/${name}.png`, fullPage: true });

const admin = { name: "Test Admin", email: "admin@example.com", password: "correct horse battery" };

test.describe.configure({ mode: "serial" });

test("fresh install redirects to setup and creates admin", async ({ page }) => {
  await page.goto("/");
  await expect(page).toHaveURL(/\/setup$/);
  await shot(page, "01-setup");
  await page.getByLabel("Your name").fill(admin.name);
  await page.getByLabel("Email").fill(admin.email);
  await page.getByLabel("Password", { exact: true }).fill(admin.password);
  await page.getByLabel("Confirm password").fill(admin.password);
  await page.getByLabel("Household name").fill("Smoke Household");
  await page.getByRole("button", { name: "Create account" }).click();
  await expect(page).toHaveURL(/\/$/);
  await expect(page.getByRole("heading", { name: "Dashboard", level: 1 })).toBeVisible();
});

test("setup page is closed after first run", async ({ page }) => {
  await page.goto("/setup");
  await expect(page).toHaveURL(/\/login$/);
});

test("login, navigate every tab, sign out", async ({ page }) => {
  await page.goto("/login");
  await page.getByLabel("Password").fill("wrong password!!");
  await page.getByLabel("Email").fill(admin.email);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page.getByText("Invalid email or password.")).toBeVisible();
  await shot(page, "02-login-error");

  await page.getByLabel("Password").fill(admin.password);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page.getByRole("heading", { name: "Dashboard", level: 1 })).toBeVisible();
  await expect(page.getByText("Smoke Household")).toBeVisible();
  await shot(page, "03-dashboard");

  for (const tab of ["Accounts", "Transactions", "Budget", "Reports", "Goals", "Settings"]) {
    await page.getByRole("link", { name: tab }).first().click();
    await expect(page.getByRole("heading", { name: tab, level: 1 })).toBeVisible();
  }
  await page.reload(); // deep link survives a reload (SPA fallback)
  await expect(page.getByRole("heading", { name: "Settings", level: 1 })).toBeVisible();

  await page.getByRole("button", { name: "Sign out" }).click();
  await expect(page).toHaveURL(/\/login$/);
  await page.goto("/budget");
  await expect(page).toHaveURL(/\/login$/);
});

test("mobile layout uses bottom tab bar", async ({ browser }) => {
  const ctx = await browser.newContext({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true });
  const page = await ctx.newPage();
  await page.goto("/login");
  await page.getByLabel("Email").fill(admin.email);
  await page.getByLabel("Password").fill(admin.password);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page.getByRole("heading", { name: "Dashboard", level: 1 })).toBeVisible();
  await page.getByRole("link", { name: "Budget" }).click();
  await expect(page.getByRole("heading", { name: "Budget", level: 1 })).toBeVisible();
  await shot(page, "04-mobile-budget");
  await ctx.close();
});

test("PWA manifest and service worker are served", async ({ request }) => {
  const m = await request.get("/manifest.webmanifest");
  expect(m.headers()["content-type"]).toBe("application/manifest+json");
  expect((await m.json()).name).toBe("Viceroy");
  expect((await request.get("/sw.js")).ok()).toBe(true);
});
