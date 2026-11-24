import { expect, test } from "@playwright/test";
import type { ParticipantTeamTaskHistory } from "../src/shared/api/generated/schemas/client/participantTeamTaskHistory.zod";
import type { Scoreboard } from "../src/shared/api/generated/schemas/client/scoreboard.zod";
import { createE2EContext } from "./support";

const teamId = "0199f7c0-0000-7000-8000-000000000001";
const taskId = "0199f7c0-0000-7000-8000-000000000099";
const otherTaskId = "0199f7c0-0000-7000-8000-000000000098";

const runtime: Scoreboard["runtime"] = {
  phase: "running",
  practice: false,
  reset_pending: false,
  generation: 4,
  scheduled_start: "2026-01-01T00:00:00Z",
  can_start: false,
  can_start_practice: false,
  round: 13,
  round_start: 1720,
  paused_at: null,
  paused_seconds: 0,
  round_time: 60,
  round_waiting: false,
  results_pending: false,
};

const scoreboard: Scoreboard = {
  state: {
    round: 12,
    round_start: 1660,
    team_tasks: [{
      team_id: teamId,
      task_id: taskId,
      status: 101,
      stolen: 0,
      lost: 0,
      score: 1000,
      checks: 8,
      checks_passed: 8,
      message: "Current state text should not be repeated",
    }],
  },
  teams: [{
    id: teamId,
    name: "Round Test Team",
    ip: "192.0.2.1",
    kind: "player",
    highlighted: false,
    active: true,
    logo_path: "",
  }],
  tasks: [{
    id: taskId,
    name: "Vault",
  }],
  config: {
    id: 1,
    flag_lifetime: 3,
    game_hardness: 2,
    inflation: false,
    volga_attacks_mode: false,
    round_time: 60,
    rounds: 50,
    mode: "classic",
    timezone: "UTC",
    start_time: runtime.scheduled_start,
    real_round: 13,
    game_running: true,
  },
  runtime,
};

function historyEntry(
  round: number,
  status: number,
  message: string | null,
  timestamp: string,
  entryTaskId = taskId,
): ParticipantTeamTaskHistory {
  return {
    team_id: teamId,
    task_id: entryTaskId,
    round,
    status,
    stolen: 0,
    lost: 0,
    round_fp_gained: null,
    round_flags_captured: null,
    round_flags_lost: null,
    round_opponent_fp: null,
    score: 1000,
    checks: 8,
    checks_passed: 8,
    timestamp,
    message,
  };
}

const history: ParticipantTeamTaskHistory[] = [
  historyEntry(5, 101, "  ", "500-1"),
  historyEntry(13, 101, "Future published round should be hidden", "1720-1"),
  {
    ...historyEntry(12, 101, "Публичный текст раунда 12", "1201-1"),
    lost: 73,
    round_fp_gained: 37.5,
    round_flags_captured: 3,
    round_flags_lost: 2,
    round_opponent_fp: 17.25,
  },
  historyEntry(2, 104, "Connection refused", "200-1"),
  historyEntry(12, 104, "Older duplicate should be hidden", "1200-1"),
  historyEntry(10, 999, "Unknown status stays neutral", "1000-1"),
  historyEntry(8, 102, "Round 8 is CORRUPT", "800-1"),
  historyEntry(11, -1, null, "1100-1"),
  {
    ...historyEntry(4, 104, null, "400-1"),
    round_fp_gained: 0,
    round_flags_captured: 0,
    round_flags_lost: 0,
    round_opponent_fp: 0,
  },
  historyEntry(9, 104, "Round 9 is DOWN", "900-1"),
  historyEntry(1, 101, "First published result", "100-1"),
  historyEntry(7, 103, "Round 7 is MUMBLE", "700-1"),
  historyEntry(6, 110, "Round 6 checker failed", "600-1"),
  historyEntry(3, 101, "Round 3 is UP", "300-1"),
  historyEntry(12, 104, "Other service should be hidden", "1202-1", otherTaskId),
];

test("a new capture appears on opposite sides of the two teams' service histories", async ({ page }) => {
  await page.goto("/?demo=1");
  const round = await page.locator(".header-round").innerText();
  await page.getByRole("button", {
    name: "Новое событие",
    exact: true,
  }).click();
  await page.getByRole("button", {
    name: /Byteforge · Vault/,
  }).click();
  const row = page.getByRole("dialog").getByRole("row").nth(1);
  const metrics = row.locator(".checker-round-metric");
  await expect(row.getByRole("rowheader")).toHaveText(round);
  await expect(metrics.nth(1)).toHaveText("1");
  const earned = await metrics.first().innerText();
  expect(earned).toMatch(/[1-9]/);
  await expect(metrics).toHaveText([earned, "1", "0", "0"]);
  await page.keyboard.press("Escape");
  await page.getByRole("button", {
    name: /Northstar · Vault/,
  }).click();
  await expect(row.getByRole("rowheader")).toHaveText(round);
  await expect(metrics).toHaveText(["0", "0", earned, "1"]);
});

test("the checker message column stays aligned across service statuses", async ({ page }) => {
  await page.emulateMedia({
    reducedMotion: "reduce",
  });
  await page.goto("/?demo=1");
  const dialog = page.getByRole("dialog");
  const offsets: number[] = [];
  for (const team of ["Northstar", "Stackline", "Null Route", "Kernel Club"]) {
    await page.getByRole("button", {
      name: new RegExp(`${team} · Market`),
    }).click();
    const message = dialog.getByText("OK", {
      exact: true,
    }).first();
    await expect(message).toBeVisible();
    const offset = await message.evaluate((label) => {
      return label.getBoundingClientRect().left - label.closest("tr")!.getBoundingClientRect().left
    });
    offsets.push(offset);
    await page.keyboard.press("Escape");
  }
  expect(Math.max(...offsets) - Math.min(...offsets)).toBeLessThan(1);
});

test("desktop capture headers fit on one line and align with their values", async ({ page }) => {
  await page.emulateMedia({
    reducedMotion: "reduce",
  });
  await page.goto("/?demo=1");
  await page.getByRole("button", {
    name: /Kernel Club · Market/,
  }).click();
  const dialog = page.getByRole("dialog");
  const history = dialog.getByRole("table");
  await expect(history.getByRole("rowheader").first()).toBeVisible();

  for (const width of [1280, 1440]) {
    await page.setViewportSize({
      width,
      height: 800,
    });
    for (const name of ["+FP", "+FLAGS", "-FP", "-FLAG"]) {
      const header = history.getByRole("columnheader", {
        name,
        exact: true,
      });
      await header.scrollIntoViewIfNeeded();
      await expect(header).toBeInViewport({
        ratio: 1,
      });
      const lines = await header.evaluate((cell) => {
        const range = document.createRange();
        range.selectNodeContents(cell);
        return range.getClientRects().length
      });
      expect(lines).toBe(1);
    }
    const geometry = await history.evaluate((table) => {
      if (!(table instanceof HTMLTableElement)) {
        throw new Error("Expected round history table");
      }
      const viewport = table.parentElement!;
      viewport.scrollLeft = viewport.scrollWidth;
      const rail = viewport.getBoundingClientRect().left + viewport.clientWidth;
      const range = document.createRange();
      const bounds = (cell: HTMLTableCellElement) => {
        range.selectNodeContents(cell);
        return range.getBoundingClientRect()
      };
      const rows = Array.from(table.rows);
      const columns = [3, 4, 5, 6];
      return {
        clearance: Math.min(...rows.map((row) => {
          return rail - bounds(row.cells[6]).right
        })),
        alignment: Math.max(...rows.flatMap((row) => {
          return columns.map((i) => {
            const cell = row.cells[i];
            const box = cell.getBoundingClientRect();
            const text = bounds(cell);
            return Math.abs((text.left + text.right - box.left - box.right) / 2)
          })
        })),
      }
    });
    expect(geometry.clearance).toBeGreaterThanOrEqual(16);
    expect(geometry.alignment).toBeLessThan(1);
    const fits = await dialog.evaluate((el) => {
      return el.scrollWidth <= el.clientWidth && document.documentElement.scrollWidth <= innerWidth
    });
    expect(fits).toBe(true);
  }
});

for (const [width, language] of [[320, "ru"], [390, "en"]] as const) {
  test(`each round fits across a ${width}px phone in ${language} without sideways scrolling`, async ({ page }) => {
    await page.setViewportSize({
      width,
      height: 844,
    });
    await page.addInitScript((lang) => {
      localStorage.setItem("forcad-language", lang);
    }, language);
    await page.goto("/?demo=1");
    await page.getByRole("button", {
      name: /Kernel Club · Market/,
    }).click();
    const list = page.getByRole("dialog").locator(".checker-round-list");
    const card = list.getByRole("row").filter({
      hasText: "110 CHECK FAILED",
    }).first();
    await expect(card.getByRole("rowheader")).toContainText(language === "ru" ? "Раунд" : "Round");
    await expect(card.getByText("The checker could not complete the check")).toBeInViewport({
      ratio: 1,
    });
    await expect(card.locator(".checker-round-metric")).toHaveCount(4);
    for (const metric of await card.locator(".checker-round-metric").all()) {
      await expect(metric).toBeInViewport({
        ratio: 1,
      });
    }
    const sizes = await list.evaluate((element) => {
      return [element.scrollWidth - element.clientWidth, document.documentElement.scrollWidth - innerWidth]
    });
    expect(sizes).toEqual([0, 0]);
  });
}

const preStartScoreboard: Scoreboard = {
  ...scoreboard,
  state: {
    ...scoreboard.state,
    round: 0,
    round_start: 0,
    team_tasks: [{
      team_id: teamId,
      task_id: taskId,
      status: -1,
      stolen: 0,
      lost: 0,
      score: 0,
      checks: 0,
      checks_passed: 0,
      message: null,
    }],
  },
  config: {
    ...scoreboard.config,
    real_round: 0,
    game_running: false,
  },
  runtime: {
    ...runtime,
    phase: "waiting",
    round: 0,
    round_start: null,
  },
};

test("scrolling a 140-round game reaches the first published result", async ({ page }) => {
  await page.setViewportSize({
    width: 1280,
    height: 800,
  });
  await page.addInitScript(() => {
    localStorage.setItem("forcad-language", "en");
  });
  const game = {
    ...scoreboard,
    state: {
      ...scoreboard.state,
      round: 140,
    },
    config: {
      ...scoreboard.config,
      rounds: 200,
      real_round: 141,
    },
    runtime: {
      ...runtime,
      round: 141,
    },
  };
  const requestedRounds: number[] = [];
  await page.routeWebSocket("**/api/v1/events/ws", () => {});
  await page.route("**/api/v1/**", async (route) => {
    const url = new URL(route.request().url());
    if (url.pathname === "/api/v1/client/scoreboard/") {
      return route.fulfill({
        json: game,
      });
    }
    if (url.pathname === "/api/v1/client/status/") {
      return route.fulfill({
        json: game.runtime,
      });
    }
    if (url.pathname === `/api/v1/client/teams/${teamId}/`) {
      const through = Number(url.searchParams.get("through_round") ?? 140);
      requestedRounds.push(through);
      const rounds = Array.from({
        length: Math.min(50, through),
      }, (_, index) => {
        const round = through - index;
        return historyEntry(round, 101, `Report ${round}`, `${round}-1`)
      });
      return route.fulfill({
        json: rounds,
      });
    }
    return route.abort();
  });

  await page.goto("/?demo=0");
  await page.locator(".scoreboard-reveal .service-cell").first().click();
  const list = page.getByRole("dialog").locator(".checker-round-list");
  const rows = list.getByRole("rowheader");
  await expect(rows.first()).toHaveText("Round 140");
  expect(requestedRounds).toEqual([140]);
  await expect(rows.nth(9)).toBeInViewport({
    ratio: 1,
  });
  await expect(rows.nth(10)).not.toBeInViewport();

  await list.press("End");
  await expect(list.getByRole("rowheader", {
    name: "Round 90",
    exact: true,
  })).toBeAttached();
  await list.press("End");
  await expect(rows.last()).toHaveText("Round 1");
  await list.press("End");
  await expect(rows.last()).toBeInViewport({
    ratio: 1,
  });
  await expect(rows).toHaveText(Array.from({
    length: 140,
  }, (_, index) => {
    return `Round ${140 - index}`
  }));
  await list.press("Home");
  await expect(rows.first()).toBeInViewport({
    ratio: 1,
  });
  expect(requestedRounds).toEqual([140, 90, 40]);
});

test("service history shows the latest published snapshots for the selected task", async ({ browser }) => {
  const context = await createE2EContext(browser);
  const page = await context.newPage();
  const apiRequests: string[] = [];
  const historyRequests: Array<{
    task: string | null;
    through: string | null;
  }> = [];
  let activeScoreboard = scoreboard;

  try {
    await page.addInitScript(() => {
      localStorage.setItem("forcad-language", "en");
    });
    await page.routeWebSocket("**/api/v1/events/ws", () => {});
    await page.route("**/api/v1/**", async (route) => {
      const url = new URL(route.request().url());
      const request = `${route.request().method()} ${url.pathname}`;
      apiRequests.push(request);

      if (url.pathname === "/api/v1/client/scoreboard/") {
        return route.fulfill({
          json: activeScoreboard,
        });
      }
      if (url.pathname === "/api/v1/client/status/") {
        return route.fulfill({
          json: activeScoreboard.runtime,
        });
      }
      if (url.pathname === `/api/v1/client/teams/${teamId}/`) {
        historyRequests.push({
          task: url.searchParams.get("task_id"),
          through: url.searchParams.get("through_round"),
        });
        return route.fulfill({
          json: history,
        });
      }

      return route.abort();
    });

    await page.goto("/?demo=0");
    const currentCell = page.locator(".scoreboard-reveal .service-cell").first();
    await expect(currentCell).toBeVisible();
    expect(historyRequests).toEqual([]);

    await currentCell.click();
    const dialog = page.getByRole("dialog");
    const summary = dialog.locator(".checker-round-summary");
    const list = summary.locator(".checker-round-list");
    const entries = list.locator(".checker-round-entry");
    await expect(entries).toHaveCount(12);

    for (const [index, round] of [12, 11, 10, 9, 8, 7, 6, 5, 4, 3, 2, 1].entries()) {
      await expect(entries.nth(index).getByRole("rowheader")).toHaveText(`Round ${round}`);
    }
    await expect(entries.nth(0)).toContainText("101");
    await expect(entries.nth(0)).toContainText("UP");
    await expect(entries.nth(2)).toContainText("999");
    await expect(entries.nth(3)).toContainText("104");
    await expect(entries.nth(3)).toContainText("DOWN");
    await expect(summary.getByText(/^(PUT|GET|CHECK)$/)).toHaveCount(0);
    await expect(summary.getByRole("button")).toHaveCount(0);
    await expect(summary.getByRole("columnheader")).toHaveText([
      "Round", "Status", "Checker response", "+FP", "+FLAGS", "-FP", "-FLAG",
    ]);
    await expect(entries.nth(0)).toContainText("Публичный текст раунда 12");
    await expect(entries.nth(0).getByRole("cell")).toHaveText([
      "101 UP", "Публичный текст раунда 12", "37.5", "3", "17.25", "2",
    ]);
    await expect(entries.nth(1).getByRole("cell")).toHaveText(["No data", "—", "—", "—", "—", "—"]);
    await expect(entries.nth(8).getByRole("cell")).toHaveText(["104 DOWN", "—", "0", "0", "0", "0"]);
    await expect(list).not.toContainText("Older duplicate should be hidden");
    await expect(dialog).not.toContainText("Current state text should not be repeated");
    await expect(list).not.toContainText("Future published round should be hidden");
    await expect(list).not.toContainText("Other service should be hidden");
    await expect(entries.last()).toContainText("First published result");

    await expect(entries.nth(1)).toContainText("No data");
    await expect(entries.nth(1)).toContainText("—");
    await expect(entries.nth(1)).not.toContainText("UP");
    await expect(entries.nth(1)).not.toContainText("OK");
    await expect(entries.nth(7).getByText("OK", {
      exact: true,
    })).toBeVisible();
    await expect(entries.nth(8)).not.toContainText("OK");
    await expect(entries.nth(8)).toContainText("—");

    const upColor = await currentCell.evaluate((cell) => {
      return getComputedStyle(cell).backgroundColor
    });
    await expect(entries.first()).toHaveCSS("background-color", upColor);
    const neutralColor = await entries.nth(1).evaluate((entry) => {
      return getComputedStyle(entry).backgroundColor
    });
    expect(neutralColor).not.toBe(upColor);
    await expect(entries.nth(2)).toHaveCSS("background-color", neutralColor);
    await expect(entries.nth(3)).not.toHaveCSS("background-color", upColor);
    await list.focus();
    await page.keyboard.press("End");
    await expect.poll(() => {
      return list.evaluate((element) => {
        return element.scrollTop
      })
    }).toBeGreaterThan(0);

    expect(historyRequests).toEqual([
      {
        task: taskId,
        through: "12",
      },
    ]);
    expect(apiRequests.some((request) => {
      return request.includes("/checks/")
    })).toBe(false);

    await dialog.press("Escape");
    await page.locator(".team-identity").click();
    await expect(page.getByRole("heading", {
      name: "Round Test Team",
      exact: true,
    })).toBeVisible();
    await expect(page.locator(".history-table tbody tr").nth(0).locator(".history-round"))
      .toHaveText("13");
    await expect(page.locator(".history-table tbody tr").nth(2).locator(".history-round"))
      .toHaveText("11");
    expect(historyRequests.at(-1)).toEqual({
      task: null,
      through: null,
    });

    const oldRoundCell = page.locator(".history-table tbody tr").nth(2).locator(".service-cell");
    await oldRoundCell.click();
    const historicalDialog = page.getByRole("dialog");
    const historicalEntries = historicalDialog.locator(
      ".checker-round-list .checker-round-entry",
    );
    await expect(historicalEntries).toHaveCount(11);
    await expect(historicalEntries.first()).toContainText("Round 11");
    await expect(historicalEntries.last().getByRole("rowheader")).toHaveText("Round 1");
    await expect(historicalDialog).not.toContainText("Round 12");
    await expect(historicalDialog).not.toContainText("Future published round should be hidden");
    expect(historyRequests.at(-1)).toEqual({
      task: taskId,
      through: "11",
    });

    await historicalDialog.press("Escape");
    activeScoreboard = preStartScoreboard;
    await page.goto("/?demo=0");
    const preStartCell = page.locator(".scoreboard-reveal .service-cell").first();
    await expect(preStartCell).toBeVisible();
    await preStartCell.click();
    const preStartDialog = page.getByRole("dialog");
    await expect(preStartDialog.getByRole("status")).toHaveText("The game has not started");
    await expect(preStartDialog.locator(".checker-round-list .checker-round-entry")).toHaveCount(0);
    await expect(preStartDialog).not.toContainText("UP");
    await expect(preStartDialog).not.toContainText("OK");
  } finally {
    await context.close();
  }
});
