import { expect, test, type Route } from "@playwright/test";
import { createE2EContext } from "./support";

const scoreboardChunk = /\/assets\/ScoreboardPage-[^/]+\.js$/;

test("a lazy route announces loading while its page chunk is pending", async ({ browser }) => {
  const context = await createE2EContext(browser);
  const page = await context.newPage();
  let releaseChunk!: () => void;
  const chunkGate = new Promise<void>((resolve) => {
    releaseChunk = resolve;
  });

  try {
    await page.route(scoreboardChunk, async (route) => {
      await chunkGate;
      await route.continue();
    });
    await page.goto("/", { waitUntil: "domcontentloaded" });
    await expect(page.getByRole("status")).toHaveAttribute("aria-busy", "true");
    releaseChunk();
    await expect(page.getByRole("heading", { name: "Рейтинг команд", exact: true }))
      .toBeVisible();
  } finally {
    releaseChunk();
    await context.close();
  }
});

test("a failed lazy route offers an accessible retry action", async ({ browser }) => {
  const context = await createE2EContext(browser);
  const page = await context.newPage();
  const failChunk = (route: Route) => route.fulfill({
    status: 503,
    headers: { "cache-control": "no-store" },
    contentType: "application/javascript",
    body: "throw new Error('Route chunk unavailable');",
  });

  try {
    await page.route(scoreboardChunk, failChunk);
    await page.goto("/");
    await expect(page.getByRole("alert")).toContainText("Страница не загрузилась");
    await expect(page.getByRole("button", { name: "Перезагрузить страницу" }))
      .toBeVisible();
    await page.unroute(scoreboardChunk, failChunk);
    await page.getByRole("button", { name: "Перезагрузить страницу" }).click();
    await expect(page.getByRole("heading", { name: "Рейтинг команд", exact: true }))
      .toBeVisible();
  } finally {
    await context.close();
  }
});
