import { expect, test } from "@playwright/test";
import { createE2EContext, loginAsAdmin, observePublicStream, readAdminCredentials } from "./support";

test("dense scoreboard spaces SLA, FP and FLAGS evenly across a service cell", async ({ page }) => {
  await page.setViewportSize({
    width: 1920,
    height: 1080
  });
  await page.goto("/?demo=1");
  const cell = page.locator(".scoreboard-reveal .service-cell").first();
  const gaps = await cell.evaluate((element) => {
    const [sla, fp, flags] = [...element.querySelectorAll(".service-metric > span")].map((label) => {
      return label.getBoundingClientRect().left
    });
    const { width } = element.getBoundingClientRect();
    return [(fp - sla) / width, (flags - fp) / width];
  });
  for (const gap of gaps) {
    expect(gap).toBeGreaterThan(0.25);
    expect(gap).toBeLessThan(0.42);
  }
});

test("a long team name fits beside score and five services on the public board", async ({ page }) => {
  await page.setViewportSize({
    width: 1568,
    height: 889,
  });
  await page.goto("/?demo=1");
  const row = page.locator(".scoreboard-reveal tbody tr").filter({
    hasText: "Signal Works Security Research Group",
  });
  await expect(row.locator(".service-column")).toHaveCount(5);
  const name = row.locator(".team-name");
  await expect.poll(() => {
    return name.evaluate((label) => {
      return label.scrollWidth - label.clientWidth
    })
  }).toBeLessThanOrEqual(1);
  const team = await name.boundingBox();
  const score = await row.locator(".total-column").boundingBox();
  const lastService = await row.locator(".service-column").last().boundingBox();
  expect(score!.x).toBeGreaterThanOrEqual(Math.floor(team!.x + team!.width));
  expect(lastService!.x + lastService!.width).toBeLessThanOrEqual(1568);
});

test("scoreboard shows ten and twenty teams before internal scrolling", async ({ browser }) => {
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
      data.state.team_tasks = teamCount === 10
        ? [{
          ...data.state.team_tasks[0],
          team_id: data.teams[1].id,
          task_id: data.tasks[0].id,
          status: 101,
          score: 112_500,
          checks: 1,
          checks_passed: 1,
        }]
        : [];
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

    teamCount = 10;
    await page.reload();
    await expect(rows).toHaveCount(10);
    await expect.poll(async () => {
      return scroll.evaluate((element) => {
        return element.scrollHeight - element.clientHeight
      })
    }).toBeLessThanOrEqual(1);
    expect(await rows.last().evaluate((row) => {
      return row.getBoundingClientRect().bottom
    })).toBeLessThanOrEqual(881);
    const labels = page.locator(".scoreboard-reveal .service-cell:not(.service-cell--active)").first().locator(".service-metric > span");
    await expect(labels).toHaveText(["SLA", "FP", "FLAGS"]);
    const contrasts = await labels.evaluateAll((elements) => {
      const background = getComputedStyle(elements[0].closest(".service-cell")!).backgroundColor.match(/[\d.]+/g)!.map(Number);
      const luminance = (channels: number[]) => {
        const linear = channels.slice(0, 3).map((channel) => {
          const value = channel / 255;
          return value <= 0.04045 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4
        });
        return linear[0] * 0.2126 + linear[1] * 0.7152 + linear[2] * 0.0722
      };
      return elements.map((element) => {
        const style = getComputedStyle(element);
        if (style.visibility !== "visible") {
          return 0
        }
        const color = style.color.match(/[\d.]+/g)!.map(Number);
        const alpha = color[3] ?? 1;
        const foreground = color.slice(0, 3).map((channel, index) => {
          return channel * alpha + background[index] * (1 - alpha)
        });
        const light = Math.max(luminance(foreground), luminance(background));
        const dark = Math.min(luminance(foreground), luminance(background));
        return (light + 0.05) / (dark + 0.05)
      });
    });
    for (const contrast of contrasts) {
      expect(contrast).toBeGreaterThanOrEqual(4.5);
    }
    await page.setViewportSize({
      width: 1215,
      height: 980
    });
    const fp = page.locator(".scoreboard-reveal .service-cell--wide-values").first().locator(".service-metric").nth(1).locator("strong");
    await expect(fp).toHaveText(/112\s500/);
    await expect.poll(async () => {
      return fp.evaluate((element) => {
        return element.scrollWidth - element.clientWidth
      })
    }).toBeLessThanOrEqual(1);
    const fpLabel = (team: number) => {
      return page.locator(`[data-team-id="00000000-0000-4000-8000-${String(team).padStart(12, "0")}"] .service-column`)
        .first().locator(".service-metric").nth(1).locator("span")
    };
    const [active, inactive] = await Promise.all([
      fpLabel(2).boundingBox(),
      fpLabel(1).boundingBox(),
    ]);
    expect(active).not.toBeNull();
    expect(inactive).not.toBeNull();
    expect(Math.abs(active!.x - inactive!.x)).toBeLessThanOrEqual(1);

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

test("large flag counts fit inside narrow desktop service cells without an unknown badge", async ({ browser }) => {
  const context = await createE2EContext(browser);
  const page = await context.newPage();
  await page.setViewportSize({
    width: 911,
    height: 980
  });

  try {
    await page.route("**/api/v1/client/scoreboard/", async (route) => {
      const response = await route.fetch();
      const data = await response.json();
      data.state.team_tasks = [{
        ...data.state.team_tasks[0],
        team_id: data.teams[0].id,
        task_id: data.tasks[0].id,
        status: 101,
        score: 12500,
        checks: 10000,
        checks_passed: 9999,
        stolen: 1000,
        lost: 1000,
      }];
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

    const cell = page.locator(".scoreboard-reveal .service-cell--wide-values").first();
    await expect(cell.locator(".service-metric strong")).toHaveText([
      /99[.,]99%/,
      /12\s500/,
      /\+1\s000\/-1\s000/,
    ]);
    await expect(page.locator(".scoreboard-reveal .service-state")).toHaveCount(0);
    const metrics = await cell.evaluate((element) => {
      const card = element.getBoundingClientRect();
      return [...element.querySelectorAll(".service-metric > span, .service-metric strong")].map((metric) => {
        const rect = metric.getBoundingClientRect();
        return {
          overflow: metric.scrollWidth - metric.clientWidth,
          right: rect.right - card.right,
          bottom: rect.bottom - card.bottom,
        };
      });
    });
    for (const metric of metrics) {
      expect(metric.overflow).toBeLessThanOrEqual(1);
      expect(metric.right).toBeLessThanOrEqual(1);
      expect(metric.bottom).toBeLessThanOrEqual(1);
    }
  } finally {
    await context.close();
  }
});

test("admin scoreboard keeps the timeline in the header and centers game settings", async ({ browser }) => {
  const context = await createE2EContext(browser);
  const page = await context.newPage();
  const credentials = await readAdminCredentials();
  await page.setViewportSize({
    width: 1536,
    height: 880
  });

  try {
    await loginAsAdmin(page, credentials);
    const settings = page.getByTestId("game-settings-trigger");
    const offset = await settings.evaluate((button) => {
      const contents = document.createRange();
      contents.selectNodeContents(button);
      const textAndIcon = contents.getBoundingClientRect();
      const box = button.getBoundingClientRect();
      return Math.abs((textAndIcon.left - box.left) - (box.right - textAndIcon.right));
    });
    expect(offset).toBeLessThanOrEqual(2);
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
