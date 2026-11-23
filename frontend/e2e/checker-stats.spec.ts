import { expect, test } from "@playwright/test";
import type { AdminCheckerStats } from "../src/shared/api/generated/schemas/admin/adminCheckerStats.zod";
import type { Scoreboard } from "../src/shared/api/generated/schemas/client/scoreboard.zod";

const taskId = "0199f7c0-0000-7000-8000-000000000099";

function count(maximum: number, reported = 0, failed = 0, noResult = 0) {
  return {
    maximum,
    reported,
    failed,
    no_result: noResult,
  };
}

test("checker cards count actions for the selected server and live round", async ({ page }) => {
  const teams: Scoreboard["teams"] = Array.from({
    length: 20
  }, (_, i) => {
    return {
      id: `0199f7c0-0000-7000-8000-${String(i + 1).padStart(12, "0")}`,
      name: `Team ${i + 1}`,
      ip: `192.0.2.${i + 1}`,
      kind: "player",
      highlighted: false,
      active: true,
      logo_path: "",
    };
  });
  const runtime: Scoreboard["runtime"] = {
    phase: "running",
    practice: false,
    reset_pending: false,
    generation: 1,
    scheduled_start: "2026-01-01T00:00:00Z",
    can_start: false,
    can_start_practice: false,
    round: 7,
    round_start: 1000,
    paused_at: null,
    paused_seconds: 0,
    round_time: 60,
    round_waiting: false,
    results_pending: false,
  };
  const board: Scoreboard = {
    runtime,
    teams,
    tasks: [{
      id: taskId,
      name: "Vault",
    }],
    state: {
      round: 6,
      round_start: 940,
      team_tasks: [],
    },
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
      real_round: 7,
      game_running: true,
    },
  };
  let secondTeamReports = 0;
  await page.routeWebSocket("**/api/v1/events/ws", () => {
    // Keep the HTTP fixtures authoritative for the test.
  });
  await page.route("**/api/v1/admin/status/", (route) => {
    return route.fulfill({
      json: {
        status: "ok",
        username: "operator",
        csrf_token: "0".repeat(64),
      },
    });
  });
  await page.route("**/api/v1/client/scoreboard/", (route) => {
    return route.fulfill({
      json: board
    });
  });
  await page.route("**/api/v1/client/status/", (route) => {
    return route.fulfill({
      json: runtime
    });
  });
  await page.route("**/api/v1/admin/checker-stats/**", (route) => {
    const params = new URL(route.request().url()).searchParams;
    const team = params.get("team_id");
    const round = Number(params.get("round"));
    if (team !== teams[0].id && team !== teams[1].id) {
      return route.fulfill({
        status: 503,
        json: {
          error: "Unavailable"
        },
      });
    }
    const first = team === teams[0].id && round === 7;
    const reported = first ? 5 : round === 7 ? secondTeamReports : 0;
    const stats: AdminCheckerStats = {
      team_id: team,
      round,
      tasks: [{
        task_id: taskId,
        name: "Vault",
        check: count(1, 1),
        put: count(5, reported, first ? 1 : 0),
        get: count(2, 0, 0, first ? 2 : 0),
      }],
    };
    return route.fulfill({
      json: stats
    });
  });

  await page.goto("/admin/scoreboard");
  const panel = page.getByRole("region", {
    name: "Чекеры"
  });
  const select = panel.getByRole("combobox", {
    name: "Команда / сервер"
  });
  const counts = panel.getByRole("article", {
    name: "Vault"
  }).getByRole("definition");
  await expect(select.locator("option")).toHaveCount(20);
  await expect(counts.nth(1)).toContainText("5/5");
  await expect(counts.nth(1)).toContainText("ошибок: 1");
  await expect(counts.nth(2)).toContainText("без ответа: 2");
  await select.selectOption(teams[1].id);
  await expect(counts.nth(1)).toHaveText("0/5");
  secondTeamReports = 2;
  await expect(counts.nth(1)).toHaveText("2/5");
  runtime.round = 8;
  await expect(panel).toContainText("Раунд 8");
  await expect(counts.nth(1)).toHaveText("0/5");
  await select.selectOption(teams[2].id);
  await expect(panel.getByRole("alert")).toBeVisible();
  await expect(counts).toHaveCount(0);
});
