import { expect, test } from "@playwright/test";
import { createE2EContext, expectDocumentSecurity, observeCSP } from "./support";

test("public team list, direct profile reload, history, and scoreboard load", async ({ browser }) => {
  const context = await createE2EContext(browser);
  const page = await context.newPage();
  const cspViolations = await observeCSP(page);

  try {
    const scoreboardResponse = page.waitForResponse((response) => {
      const url = new URL(response.url());
      return url.pathname === "/api/client/scoreboard/" && response.request().method() === "GET";
    });
    expectDocumentSecurity(await page.goto("/teams"));
    expect((await scoreboardResponse).status()).toBe(200);
    await expect(page.getByRole("region", {
      name: "Профили и результаты"
    })).toBeVisible();

    await expect(page.locator(".teams-page .page-hero")).toHaveCount(0);
    await expect(page.getByRole("contentinfo")).toHaveCount(0);

    const teamLinks = page.locator('a[href^="/team/"]');
    await expect(teamLinks).toHaveCount(2);
    const firstTeam = teamLinks.filter({
      hasText: "first"
    });
    const secondTeam = teamLinks.filter({
      hasText: "second"
    });
    await expect(firstTeam).toHaveCount(1);
    await expect(secondTeam).toHaveCount(1);
    const href = await firstTeam.getAttribute("href");
    const teamId = /^\/team\/([0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12})$/i.exec(href ?? "")?.[1];
    expect(teamId).toBeDefined();

    const historyResponse = page.waitForResponse((response) => {
      const url = new URL(response.url());
      return url.pathname === `/api/client/teams/${teamId}/` && response.request().method() === "GET";
    });
    await page.goto(`/team/${teamId}`);
    expect((await historyResponse).status()).toBe(200);
    await expect(page.getByRole("heading", {
      name: "first",
      exact: true
    })).toBeVisible();
    await expect(page.getByRole("heading", {
      name: "История раундов",
      exact: true
    })).toBeVisible();
    await expect(page.getByRole("region", {
      name: "История раундов команды"
    })).toBeVisible();
    await expect(page.getByRole("table", {
      name: "История результатов команды по раундам"
    })).toBeVisible();

    const directReloadHistory = page.waitForResponse((response) => {
      const url = new URL(response.url());
      return url.pathname === `/api/client/teams/${teamId}/` && response.request().method() === "GET";
    });
    await page.reload();
    expect((await directReloadHistory).status()).toBe(200);
    await expect(page.getByRole("heading", {
      name: "first",
      exact: true
    })).toBeVisible();

    const rootScoreboard = page.waitForResponse((response) => {
      const url = new URL(response.url());
      return url.pathname === "/api/client/scoreboard/" && response.request().method() === "GET";
    });
    await page.goto("/");
    expect((await rootScoreboard).status()).toBe(200);
    await expect(page.getByRole("banner").getByRole("progressbar")).toBeVisible();
    await expect(page.getByRole("progressbar")).toHaveCount(1);
    await expect(page.locator(".delta-column, .scoreboard-update-layer, .table-hint, .recent-events--strip")).toHaveCount(0);
    await expect(page.getByRole("heading", {
      name: "Рейтинг команд",
      exact: true
    })).toBeVisible();
    expect(cspViolations).toEqual([]);
  } finally {
    await context.close();
  }
});

test("opening a scrolled team starts at the heading and Back restores the scoreboard", async ({ browser }) => {
  const context = await createE2EContext(browser);
  const page = await context.newPage();
  await page.setViewportSize({
    // Mobile cards keep the second fixture team below the fold.
    width: 700,
    height: 220
  });

  try {
    await page.goto("/");
    const team = page.locator(".scoreboard-row .team-identity").last();
    await expect(team).toBeVisible();
    await team.scrollIntoViewIfNeeded();
    await expect.poll(() => {
      return page.evaluate(() => {
        return window.scrollY;
      });
    }).toBeGreaterThan(0);
    const outgoingPosition = await page.evaluate(() => {
      return window.scrollY;
    });
    const header = await page.getByRole("banner").elementHandle();

    await team.click();
    await expect(page).toHaveURL(/\/team\/[0-9a-f-]+$/);
    await expect.poll(() => {
      return page.evaluate(() => {
        return window.scrollY;
      });
    }).toBe(0);
    await expect(page.getByRole("banner")).toBeInViewport();
    expect(await header?.evaluate((element) => {
      return element.isConnected;
    })).toBe(true);

    await page.goBack();
    await expect(page).toHaveURL(/\/$/);
    await expect.poll(async () => {
      return Math.abs(await page.evaluate(() => {
        return window.scrollY;
      }) - outgoingPosition);
    }).toBeLessThan(1);
    expect(await header?.evaluate((element) => {
      return element.isConnected;
    })).toBe(true);
  } finally {
    await context.close();
  }
});

test("service dialog releases its portal and pointer lock after keyboard and preference changes", async ({ browser }) => {
  const context = await createE2EContext(browser);
  const page = await context.newPage();
  try {
    await page.goto("/");
    const trigger = page.locator(".service-cell").first();
    await trigger.click();
    await expect(page.getByRole("dialog")).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(page.getByRole("dialog")).toHaveCount(0);
    await expect(page.locator(".ui-dialog-overlay")).toHaveCount(0);
    await expect.poll(() => {
      return page.evaluate(() => {
        return getComputedStyle(document.body).pointerEvents;
      });
    }).not.toBe("none");

    await trigger.focus();
    await page.keyboard.press("Enter");
    await expect(page.getByRole("dialog")).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(page.getByRole("dialog")).toHaveCount(0);

    await trigger.click();
    await expect(page.getByRole("dialog")).toBeVisible();
    await page.evaluate(() => {
      document.addEventListener("animationstart", function disableOnExit(event) {
        if (event.animationName.endsWith("-out")) {
          document.removeEventListener("animationstart", disableOnExit);
          document.documentElement.dataset.motion = "off";
        }
      });
    });
    await page.locator(".ui-dialog-close").click();
    await expect(page.getByRole("dialog")).toHaveCount(0);
    await expect(page.locator(".ui-dialog-overlay")).toHaveCount(0);
    await expect.poll(() => {
      return page.evaluate(() => {
        return getComputedStyle(document.body).pointerEvents;
      });
    }).not.toBe("none");
    expect(await page.evaluate(() => {
      return document.getElementById("root")?.getAttribute("aria-hidden");
    })).toBeNull();
  } finally {
    await context.close();
  }
});

test("service dialog stays centered throughout its opening animation", async ({ browser }) => {
  const context = await createE2EContext(browser);
  const page = await context.newPage();
  try {
    await page.goto("/");
    await page.evaluate(() => {
      document.addEventListener("animationstart", function pauseDialogOpening(event) {
        if (!(event.target instanceof HTMLElement) || event.target.getAttribute("role") !== "dialog") {
          return;
        }
        const animation = event.target.getAnimations().find((candidate) => {
          return candidate instanceof CSSAnimation && candidate.animationName === event.animationName;
        });
        if (animation) {
          animation.pause();
          animation.currentTime = 0;
          document.removeEventListener("animationstart", pauseDialogOpening);
        }
      });
    });
    await page.locator(".service-cell").first().click();
    const dialog = page.getByRole("dialog");
    await expect.poll(() => {
      return dialog.evaluate((element) => {
        return element.getAnimations().some((animation) => {
          return animation.playState === "paused";
        });
      });
    }).toBe(true);

    const positions = await dialog.evaluate((element) => {
      const animation = element.getAnimations().find((candidate) => {
        return candidate.playState === "paused";
      })!;
      const duration = Number(animation.effect!.getComputedTiming().duration);
      const positions = [0, duration / 2, duration].map((time) => {
        animation.currentTime = time;
        const bounds = element.getBoundingClientRect();
        return {
          time,
          horizontalOffset: bounds.x + bounds.width / 2 - window.innerWidth / 2,
          verticalOffset: bounds.y + bounds.height / 2 - window.innerHeight / 2,
        };
      });
      animation.currentTime = 0;
      return positions;
    });
    for (const position of positions) {
      expect(Math.abs(position.horizontalOffset), `horizontal center at ${position.time}ms`).toBeLessThan(1);
      expect(position.verticalOffset, `vertical center at ${position.time}ms`).toBeGreaterThanOrEqual(-1);
      expect(position.verticalOffset, `vertical center at ${position.time}ms`).toBeLessThanOrEqual(5);
    }
    await dialog.evaluate(async (element) => {
      const animation = element.getAnimations().find((candidate) => {
        return candidate.playState === "paused";
      })!;
      animation.play();
      await animation.finished;
    });
    await page.keyboard.press("Escape");
    await expect(dialog).toHaveCount(0);
    await expect(page.locator(".ui-dialog-overlay")).toHaveCount(0);
  } finally {
    await context.close();
  }
});

test("service dialog keeps its geometry while checker logs load", async ({ browser }) => {
  const context = await createE2EContext(browser);
  const page = await context.newPage();
  let releaseLogs!: () => void;
  const gate = new Promise<void>((resolve) => {
    releaseLogs = resolve;
  });
  try {
    await page.route(/\/api\/client\/teams\/[^/]+\/tasks\/[^/]+\/logs\/?(?:\?.*)?$/, async (route) => {
      await gate;
      await route.continue();
    });
    await page.goto("/");
    await page.locator(".service-cell").first().click();
    const dialog = page.getByRole("dialog");
    const history = page.locator(".service-inspector-history");
    await expect(history).toHaveAttribute("aria-busy", "true");
    await dialog.evaluate(async (element) => {
      await Promise.all(element.getAnimations().map((animation) => {
        return animation.finished;
      }));
    });
    const before = await dialog.boundingBox();
    expect(before).not.toBeNull();
    releaseLogs();
    await expect(history).toHaveAttribute("aria-busy", "false");
    const after = await dialog.boundingBox();
    expect(after).not.toBeNull();
    for (const key of ["x", "y", "width", "height"] as const) {
      expect(Math.abs(after![key] - before![key])).toBeLessThan(1);
    }
    await page.keyboard.press("Escape");
    await expect(dialog).toHaveCount(0);
  } finally {
    releaseLogs();
    await context.close();
  }
});
