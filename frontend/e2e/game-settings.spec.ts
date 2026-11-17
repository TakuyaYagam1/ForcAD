import { env } from "node:process";
import { expect, test, type Page, type Route } from "@playwright/test";
import { createE2EContext, loginAsAdmin, readAdminCredentials } from "./support";
interface SettingsFixture {
  config: {
    id: number;
    flag_lifetime: number;
    game_hardness: number;
    inflation: boolean;
    volga_attacks_mode: boolean;
    round_time: number;
    rounds: number | null;
    mode: string;
    timezone: string;
    start_time: string;
    real_round: number | null;
    game_running: boolean | null;
  };
  runtime: {
    phase: "waiting" | "running" | "paused" | "finished";
    practice: boolean;
    reset_pending: boolean;
    generation: number;
    scheduled_start: string;
    can_start: boolean;
    can_start_practice: boolean;
    round: number;
    round_start: number | null;
    paused_at: number | null;
    paused_seconds: number;
    round_time: number;
    round_waiting: boolean;
    results_pending: boolean;
  };
  extra_checks: number;
}

const baseSettings: SettingsFixture = {
  config: {
    id: 1,
    flag_lifetime: 60,
    game_hardness: 1.25,
    inflation: true,
    volga_attacks_mode: false,
    round_time: 120,
    rounds: 300,
    mode: "classic",
    timezone: "Europe/Moscow",
    start_time: "2026-11-17T09:30:00+03:00",
    real_round: 42,
    game_running: true,
  },
  runtime: {
    phase: "running",
    practice: false,
    reset_pending: false,
    generation: 7,
    scheduled_start: "2026-11-17T09:30:00+03:00",
    can_start: false,
    can_start_practice: false,
    round: 42,
    round_start: 1_795_000_000,
    paused_at: null,
    paused_seconds: 0,
    round_time: 120,
    round_waiting: false,
    results_pending: false,
  },
  extra_checks: 2,
};

async function openSettings(page: Page) {
  await page.getByTestId("game-settings-trigger").click();
  await expect(page.getByTestId("game-settings-form")).toBeVisible();
}

async function handleSettingsRoute(
  route: Route,
  settings: SettingsFixture,
  onUpdate: (body: Record<string, unknown>) => void,
) {
  if (route.request().method() === "PUT") {
    const body = route.request().postDataJSON() as Record<string, unknown>;
    onUpdate(body);
    settings.config.rounds = body.rounds as number | null;
    await route.fulfill({
      contentType: "application/json",
      body: JSON.stringify(settings),
    });
    return;
  }

  await route.fulfill({
    contentType: "application/json",
    body: JSON.stringify(settings),
  });
}

test("running game permits only a round-limit update and preserves the remaining config", async ({ browser }) => {
  const context = await createE2EContext(browser);
  const page = await context.newPage();
  const credentials = env.FORCAD_E2E_DEMO === "1"
    ? {
      username: "demo",
      password: "demo"
    }
    : await readAdminCredentials();
  const settings = structuredClone(baseSettings);
  const updates: Array<Record<string, unknown>> = [];

  try {
    await loginAsAdmin(page, credentials);
    if (env.FORCAD_E2E_DEMO !== "1") {
      await page.route("**/api/v1/admin/game/config/", async (route) => {
        await handleSettingsRoute(route, settings, (body) => {
          updates.push(body);
        });
      });
    }

    await openSettings(page);
    await expect(page.getByTestId("game-settings-rounds")).toBeEnabled();
    await expect(page.getByTestId("game-settings-round-time")).toBeDisabled();
    await expect(page.getByTestId("game-settings-mode")).toBeDisabled();
    await expect(page.getByTestId("game-settings-start-time")).toBeDisabled();

    await page.getByTestId("game-settings-rounds").fill("360");
    await page.getByTestId("game-settings-submit").click();
    await expect(page.getByTestId("admin-notification")).toContainText("Настройки сохранены.");
    if (env.FORCAD_E2E_DEMO === "1") {
      await expect(page.getByTestId("game-settings-rounds")).toHaveValue("360");
      await page.getByTestId("game-settings-cancel").click();
      await openSettings(page);
      await expect(page.getByTestId("game-settings-rounds")).toHaveValue("360");
      await expect(page.getByTestId("game-settings-round-time")).toHaveValue("120");
    } else {
      await expect.poll(() => {
        return updates.length
      }).toBe(1);
      expect(updates[0]).toEqual({
        generation: 7,
        rounds: 360,
        flag_lifetime: 60,
        game_hardness: 1.25,
        inflation: true,
        volga_attacks_mode: false,
        round_time: 120,
        extra_checks: 2,
        mode: "classic",
        timezone: "Europe/Moscow",
        start_time: "2026-11-17T06:30:00.000Z",
      });
    }
  } finally {
    await context.close();
  }
});

test("invalid settings and finished round limits never issue an update", async ({ browser }) => {
  const context = await createE2EContext(browser);
  const page = await context.newPage();
  const credentials = env.FORCAD_E2E_DEMO === "1"
    ? {
      username: "demo",
      password: "demo"
    }
    : await readAdminCredentials();
  const settings = structuredClone(baseSettings);
  settings.runtime.phase = "waiting";
  settings.runtime.can_start = true;
  settings.config.game_running = false;
  let updates = 0;

  try {
    await loginAsAdmin(page, credentials);
    if (env.FORCAD_E2E_DEMO === "1") {
      await page.locator(".demo-panel").getByRole("button", {
        name: "До старта"
      }).click();
    } else {
      await page.route("**/api/v1/admin/game/config/", async (route) => {
        await handleSettingsRoute(route, settings, () => {
          updates += 1;
        });
      });
    }

    await openSettings(page);
    await expect(page.getByTestId("game-settings-round-time")).toBeEnabled();
    await page.getByTestId("game-settings-rounds").fill(env.FORCAD_E2E_DEMO === "1" ? "0" : "42");
    await page.getByTestId("game-settings-submit").click();
    await expect(page.getByRole("alert").first()).toBeVisible();
    expect(updates).toBe(0);

    await page.getByTestId("game-settings-rounds").fill("300");
    await page.getByTestId("game-settings-hardness").fill("1");
    await page.getByTestId("game-settings-submit").click();
    await expect(page.getByRole("alert").first()).toBeVisible();
    expect(updates).toBe(0);

    await page.getByTestId("game-settings-hardness").fill("1.25");
    await page.getByTestId("game-settings-extra-checks").fill("9");
    await page.getByTestId("game-settings-submit").click();
    await expect(page.getByRole("alert").first()).toBeVisible();
    expect(updates).toBe(0);

    await page.getByTestId("game-settings-cancel").click();
    if (env.FORCAD_E2E_DEMO === "1") {
      await page.getByRole("button", {
        name: "Начать финал"
      }).first().click();
      await page.getByRole("dialog").getByRole("button", {
        name: "Начать финал"
      }).click();
      await page.getByRole("button", {
        name: "Завершить игру"
      }).first().click();
      await page.getByRole("dialog").getByRole("button", {
        name: "Завершить игру"
      }).click();
    } else {
      settings.runtime.phase = "finished";
      settings.runtime.can_start = false;
    }
    await openSettings(page);
    await expect(page.getByTestId("game-settings-rounds")).toBeDisabled();
    await expect(page.getByTestId("game-settings-submit")).toBeDisabled();
    expect(updates).toBe(0);
  } finally {
    await context.close();
  }
});
