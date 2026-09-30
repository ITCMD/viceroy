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

test("butterfly logo toggle", async ({ page }) => {
  await login(page);
  const sidebarLogo = page.locator("aside img").first();
  const favicon = page.locator('link[rel="icon"]');
  await expect(sidebarLogo).toHaveAttribute("src", "/logo-butterfly.png");
  await expect(favicon).toHaveAttribute("href", "/favicon-butterfly.png");
  await page.screenshot({ path: `${shots}/09-butterfly.png` });

  await page.goto("/settings");
  const toggle = page.getByRole("switch", { name: /Butterfly logo/ });
  await expect(toggle).toBeChecked();
  await toggle.click();
  await expect(sidebarLogo).toHaveAttribute("src", "/icon.svg");
  await expect(favicon).toHaveAttribute("href", "/icon.svg");

  // The choice sticks across a reload and on the sign-in page.
  await page.reload();
  await expect(favicon).toHaveAttribute("href", "/icon.svg");
  await page.getByRole("button", { name: "Sign out" }).click();
  await expect(page.locator("main img, img").first()).toHaveAttribute("src", "/icon.svg");

  await login(page);
  await page.goto("/settings");
  await page.getByRole("switch", { name: /Butterfly logo/ }).click();
  await expect(sidebarLogo).toHaveAttribute("src", "/logo-butterfly.png");
});
