import { expect, test } from "@playwright/test";
import { createE2EContext, observePublicStream } from "./support";

test("a highlighted team only receives a row background while hovered", async ({ browser }) => {
  const context = await createE2EContext(browser);
  const page = await context.newPage();
  try {
    await page.route("**/api/v1/client/scoreboard/", async (route) => {
      const response = await route.fetch();
      const data = await response.json();
      for (const team of data.teams) {
        team.highlighted = true;
      }
      await route.fulfill({
        response,
        json: data
      });
    });
    const stream = await observePublicStream(page);
    stream.suppressInits(true);
    await page.goto("/");
    const row = page.locator(".scoreboard-row").first();
    await expect(row).toBeVisible();
    await page.mouse.move(0, 0);
    const canvas = await page.locator(".brand-app").evaluate((element) => {
      return getComputedStyle(element).backgroundColor;
    });
    const background = () => {
      return row.evaluate((element) => {
        return getComputedStyle(element).backgroundColor;
      });
    };
    await expect.poll(background).toBe(canvas);
    await row.locator(".team-identity").hover();
    await expect.poll(background).not.toBe(canvas);
    await page.mouse.move(0, 0);
    await expect.poll(background).toBe(canvas);
  } finally {
    await context.close();
  }
});

test("team round history fits desktop and mobile without horizontal scrolling", async ({ browser }) => {
  const context = await createE2EContext(browser);
  const page = await context.newPage();
  try {
    await page.goto("/teams");
    // The isolated backend starts between games, so give this layout check
    // completed rounds without starting a game or changing shared server data.
    const scoreboard = await page.request.get("/api/v1/client/scoreboard/");
    const { tasks } = await scoreboard.json() as {
      tasks: Array<{
        id: string
      }>;
    };
    await page.route(/\/api\/v1\/client\/teams\/[^/]+\/$/, async (route) => {
      const teamId = new URL(route.request().url()).pathname.split("/")[5];
      await route.fulfill({
        json: Array.from({
          length: 6
        }, (_, index) => {
          return tasks.map((task) => {
            return {
              team_id: teamId,
              task_id: task.id,
              round: 6 - index,
              status: 101,
              stolen: 123456789,
              lost: 987654321,
              score: 1234567890.12,
              checks: 10,
              checks_passed: 9,
              timestamp: `${1794820800 - index}-0`,
              message: null,
            };
          });
        }).flat(),
      });
    });
    await page.locator('a[href^="/team/"]').first().click();
    const region = page.getByRole("region", {
      name: "История раундов команды"
    });
    await expect(region).toBeVisible();
    await expect(region.locator(".history-round").first()).toBeVisible();
    for (const width of [1440, 390, 320]) {
      await page.setViewportSize({
        width,
        height: 900
      });
      await expect.poll(() => {
        return region.evaluate((element) => {
          return element.scrollWidth - element.clientWidth;
        });
      }).toBeLessThanOrEqual(1);
      expect(await page.evaluate(() => {
        return document.documentElement.scrollWidth <= innerWidth;
      })).toBe(true);
    }
    await region.locator(".service-cell").first().click();
    await expect(page.getByRole("dialog")).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(page.getByRole("dialog")).toHaveCount(0);
  } finally {
    await context.close();
  }
});
