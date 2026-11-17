import { expect, test } from "@playwright/test";
import { createE2EContext, loginAsAdmin, observePublicStream, readAdminCredentials } from "./support";

test("scoreboard shows twenty teams before internal scrolling", async ({ browser }) => {
  const context = await createE2EContext(browser);
  const page = await context.newPage();
  await page.setViewportSize({
    width: 1536,
    height: 880
  });
  let teamCount = 20;

  try {
    await page.route("**/api/v1/client/scoreboard/", async (route) => {
      const response = await route.fetch();
      const data = await response.json();
      const template = data.teams[0];
      data.teams = Array.from({
        length: teamCount
      }, (_, index) => {
        return {
          ...template,
          id: `00000000-0000-4000-8000-${String(index + 1).padStart(12, "0")}`,
          name: `Team ${index + 1}`,
          ip: `10.183.${index + 101}.2`,
        }
      });
      data.state.team_tasks = [];
      data.runtime.phase = "finished";
      data.runtime.results_pending = false;
      await route.fulfill({
        response,
        json: data
      });
    });
    const stream = await observePublicStream(page);
    stream.suppressInits(true);
    await page.goto("/");

    const rows = page.locator(".scoreboard-reveal tbody tr");
    const scroll = page.locator(".scoreboard-reveal .table-scroll");
    await expect(rows).toHaveCount(20);
    await expect.poll(async () => {
      return page.locator(".scoreboard-page").evaluate((element) => {
        return (element as HTMLElement).style.getPropertyValue("--scoreboard-row-height")
      })
    }).not.toBe("");
    expect(await scroll.evaluate((element) => {
      return element.scrollHeight - element.clientHeight
    })).toBeLessThanOrEqual(1);
    expect(await rows.last().evaluate((row) => {
      return row.getBoundingClientRect().bottom
    })).toBeLessThanOrEqual(881);
    const header = page.locator(".site-header");
    const [brandBox, progressBox, headerBox, tableBox] = await Promise.all([
      header.locator(".brand-lockup").boundingBox(),
      page.getByRole("progressbar").boundingBox(),
      header.boundingBox(),
      page.locator(".scoreboard-reveal").boundingBox()
    ]);
    expect(brandBox).not.toBeNull();
    expect(progressBox).not.toBeNull();
    expect(headerBox).not.toBeNull();
    expect(tableBox).not.toBeNull();
    expect(progressBox!.x).toBeGreaterThan(brandBox!.x + brandBox!.width);
    expect(await header.locator(".header-progress").innerText()).not.toMatch(/\d+%/);
    expect(progressBox!.y + progressBox!.height).toBeLessThanOrEqual(headerBox!.y + headerBox!.height);
    expect(progressBox!.width).toBeLessThanOrEqual(320);
    expect(await page.evaluate(() => {
      return document.documentElement.scrollHeight - window.innerHeight
    })).toBe(0);
    await page.setViewportSize({
      width: 2514,
      height: 1427
    });
    await expect.poll(async () => {
      return Number.parseFloat(await page.locator(".scoreboard-page").evaluate((element) => {
        return (element as HTMLElement).style.getPropertyValue("--scoreboard-row-height")
      }))
    }).toBeGreaterThan(50);
    expect(await page.evaluate(() => {
      return document.documentElement.scrollHeight - window.innerHeight
    })).toBe(0);
    await page.setViewportSize({
      width: 1536,
      height: 880
    });

    teamCount = 21;
    await page.reload();
    await expect(rows).toHaveCount(21);
    expect(await scroll.evaluate((element) => {
      return element.scrollHeight - element.clientHeight
    })).toBeGreaterThan(1);
  } finally {
    await context.close();
  }
});

test("admin scoreboard moves the round timeline into the header", async ({ browser }) => {
  const context = await createE2EContext(browser);
  const page = await context.newPage();
  const credentials = await readAdminCredentials();
  await page.setViewportSize({
    width: 1536,
    height: 880
  });

  try {
    await loginAsAdmin(page, credentials);
    const header = page.locator(".site-header");
    const [brandBox, progressBox, headerBox, dashboardBox] = await Promise.all([
      header.locator(".brand-lockup").boundingBox(),
      page.getByRole("progressbar").boundingBox(),
      header.boundingBox(),
      page.locator(".admin-dashboard-strip").boundingBox()
    ]);
    expect(brandBox).not.toBeNull();
    expect(progressBox).not.toBeNull();
    expect(headerBox).not.toBeNull();
    expect(dashboardBox).not.toBeNull();
    expect(progressBox!.x).toBeGreaterThan(brandBox!.x + brandBox!.width);
    expect(progressBox!.y + progressBox!.height).toBeLessThanOrEqual(headerBox!.y + headerBox!.height);
    expect(progressBox!.width).toBeLessThanOrEqual(320);
    expect(progressBox!.width).toBeGreaterThanOrEqual(120);
    expect(await header.locator(".header-progress").innerText()).not.toMatch(/\d+%/);
  } finally {
    await context.close();
  }
});
