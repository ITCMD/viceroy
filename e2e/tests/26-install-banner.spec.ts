import { devices, expect, test } from "@playwright/test";

const shots = process.env.SCREENSHOT_DIR ?? "test-results/screens";
const admin = { email: "admin@example.com", password: "correct horse battery" };

test("phones are offered to install Viceroy; desktops aren't", async ({ browser, page }) => {
  await page.goto("/login");
  await page.getByLabel("Email").fill(admin.email);
  await page.getByLabel("Password").fill(admin.password);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page.getByRole("heading", { name: "Dashboard", level: 1 })).toBeVisible();
  await expect(page.getByTestId("install-banner")).toHaveCount(0);
  const cookies = await page.context().cookies();

  const phone = await browser.newContext({ ...devices["Pixel 7"] });
  await phone.addCookies(cookies);
  const p = await phone.newPage();
  await p.goto("/");
  const banner = p.getByTestId("install-banner");
  // No install prompt from the browser yet: point at the browser menu.
  await expect(banner).toContainText("Install app or Add to Home screen");

  // Chrome hands over its prompt: the banner gets an Install button that shows it.
  await p.evaluate(() => {
    const e = new Event("beforeinstallprompt") as Event & Record<string, unknown>;
    e.prompt = async () => ((window as unknown as Record<string, boolean>).prompted = true);
    e.userChoice = Promise.resolve({ outcome: "accepted" });
    window.dispatchEvent(e);
  });
  await expect(banner.getByRole("button", { name: "Install" })).toBeVisible();
  await p.screenshot({ path: `${shots}/26-install-banner.png` });
  await banner.getByRole("button", { name: "Install" }).click();
  await expect.poll(() => p.evaluate(() => (window as unknown as Record<string, boolean>).prompted)).toBe(true);
  await expect(banner).toHaveCount(0);

  // "Not now" keeps it away across reloads.
  await p.reload();
  await banner.getByRole("button", { name: "Not now" }).click();
  await expect(banner).toHaveCount(0);
  await p.reload();
  await expect(p.getByRole("heading", { name: "Dashboard", level: 1 })).toBeVisible();
  await expect(banner).toHaveCount(0);
  await phone.close();
});
