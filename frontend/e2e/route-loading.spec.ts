import { expect, test, type Route } from "@playwright/test";
import { createE2EContext } from "./support";

const adminLoginChunk = /\/assets\/AdminLoginPage-[^/]+\.js$/;

test("a lazy route announces loading while its page chunk is pending", async ({ browser }) => {
  const context = await createE2EContext(browser);
  const page = await context.newPage();
  let releaseChunk!: () => void;
  const chunkGate = new Promise<void>((resolve) => {
    releaseChunk = resolve;
  });

  try {
    await page.route(adminLoginChunk, async (route) => {
      await chunkGate;
      await route.continue();
    });
    await page.goto("/admin/login", {
      waitUntil: "domcontentloaded"
    });
    await expect(page.getByRole("main").getByRole("status")).toHaveAttribute("aria-busy", "true");
    await expect(page.getByRole("banner")).toBeVisible();
    await expect(page.getByRole("contentinfo")).toBeVisible();
    const header = await page.getByRole("banner").elementHandle();
    releaseChunk();
    await expect(page.locator("#username")).toBeVisible();
    expect(await header?.evaluate((element) => {
      return element.isConnected;
    })).toBe(true);
    await expect(page.getByRole("main")).toHaveCount(1);
  } finally {
    releaseChunk();
    await context.close();
  }
});

test("a failed lazy route offers an accessible retry action", async ({ browser }) => {
  const context = await createE2EContext(browser);
  const page = await context.newPage();
  const failChunk = (route: Route) => {
    return route.fulfill({
      status: 503,
      headers: {
        "cache-control": "no-store"
      },
      contentType: "application/javascript",
      body: "throw new Error('Route chunk unavailable');",
    })
  };

  try {
    await page.route(adminLoginChunk, failChunk);
    await page.goto("/admin/login");
    await expect(page.getByRole("alert")).toContainText("Страница не загрузилась");
    await expect(page.getByRole("banner")).toBeVisible();
    await expect(page.getByRole("contentinfo")).toBeVisible();
    await expect(page.getByRole("main")).toHaveCount(1);
    await expect(page.getByRole("button", {
      name: "Перезагрузить страницу"
    }))
      .toBeVisible();
    await page.unroute(adminLoginChunk, failChunk);
    await page.getByRole("button", {
      name: "Перезагрузить страницу"
    }).click();
    await expect(page.locator("#username")).toBeVisible();
  } finally {
    await context.close();
  }
});
