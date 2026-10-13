import { expect, test } from "@playwright/test";
import {
  createE2EContext,
  currentRuntime,
  expectNewPhase,
  loginAsAdmin,
  observePublicStream,
  readAdminCredentials,
  resetPractice,
} from "./support";

interface ScoreboardResponse {
  teams: Array<{ id: string; name: string }>;
  tasks: Array<{ id: string; name: string }>;
  runtime: { phase: string; practice: boolean };
}

test("public stream resynchronizes after a missed practice pause", async ({ browser }) => {
  test.setTimeout(300_000);
  const credentials = await readAdminCredentials();
  const publicContext = await createE2EContext(browser);
  const adminContext = await createE2EContext(browser);
  const publicPage = await publicContext.newPage();
  const adminPage = await adminContext.newPage();
  const pageErrors: string[] = [];
  publicPage.on("pageerror", (error) => pageErrors.push(error.message));
  adminPage.on("pageerror", (error) => pageErrors.push(error.message));
  const observation = await observePublicStream(publicPage);
  let adminAuthenticated = false;

  try {
    const initialScoreboard = publicPage.waitForResponse((response) => {
      const url = new URL(response.url());
      return url.pathname === "/api/client/scoreboard/" && response.request().method() === "GET";
    });
    const initialRuntime = publicPage.waitForResponse((response) => {
      const url = new URL(response.url());
      return url.pathname === "/api/client/status/" && response.request().method() === "GET";
    });
    await publicPage.goto("/");
    await expect(publicPage.getByRole("heading", { name: "Рейтинг команд", exact: true })).toBeVisible();
    const [scoreboardResponse, runtimeResponse] = await Promise.all([
      initialScoreboard,
      initialRuntime,
    ]);
    await expect.poll(() => observation.connections).toBeGreaterThan(0);
    expect(scoreboardResponse.status()).toBe(200);
    expect(runtimeResponse.status()).toBe(200);
    expect((await runtimeResponse.json() as { phase?: unknown }).phase).toBe("waiting");
    const initialScoreboardBody = await scoreboardResponse.json() as ScoreboardResponse;
    const firstTeam = initialScoreboardBody.teams.find((team) => team.name === "first");
    const firstTask = initialScoreboardBody.tasks[0];
    expect(firstTeam).toBeDefined();
    expect(firstTask).toBeDefined();
    if (!firstTeam || !firstTask) throw new Error("The local E2E scoreboard fixture is incomplete.");

    await loginAsAdmin(adminPage, credentials);
    adminAuthenticated = true;
    await resetPractice(adminPage);
    await expect(adminPage.getByRole("button", {
      name: "Начать тестовую игру",
      exact: true,
    })).toBeEnabled();

    const runningBeforeStart = observation.phases.filter((phase) => phase === "running").length;
    await adminPage.getByRole("button", { name: "Начать тестовую игру", exact: true }).click();
    await adminPage.getByRole("dialog").getByRole("button", {
      name: "Начать тестовую игру",
      exact: true,
    }).click();
    await expectNewPhase(observation, "running", runningBeforeStart);
    const staleScore = 9_876_543;
    observation.sendUpdate({
      round_start: 1,
      round: 999,
      team_tasks: [{
        team_id: firstTeam.id,
        task_id: firstTask.id,
        status: 101,
        stolen: 0,
        lost: 0,
        score: staleScore,
        checks: 1,
        checks_passed: 1,
        message: "",
      }],
    });
    const firstTaskScore = publicPage.locator(
      `[data-team-id="${firstTeam.id}"] .service-score > span`,
    ).first();
    await expect.poll(async () =>
      (await firstTaskScore.textContent() ?? "").replace(/\D/g, ""),
    ).toBe(String(staleScore));
    await expect(adminPage.getByText(/Пробная игра\./)).toBeVisible();
    await expect(adminPage.getByRole("button", {
      name: "Приостановить",
      exact: true,
    })).toBeEnabled();

    const connectionsBeforeOffline = observation.connections;
    await publicContext.setOffline(true);
    await observation.disconnect();
    await expect(publicPage.getByRole("banner")
      .getByText("Нет соединения", { exact: true })).toBeVisible();

    await adminPage.getByRole("button", { name: "Приостановить", exact: true }).click();
    await expect(adminPage.getByRole("button", {
      name: "Продолжить",
      exact: true,
    })).toBeEnabled();
    await expect(adminPage.getByRole("status").filter({
      hasText: "Игра приостановлена:",
    })).toBeVisible();

    const resynchronized = publicPage.waitForResponse((response) => {
      const url = new URL(response.url());
      return url.pathname === "/api/client/scoreboard/" && response.request().method() === "GET";
    }, { timeout: 60_000 });
    const resynchronizedRuntime = publicPage.waitForResponse((response) => {
      const url = new URL(response.url());
      return url.pathname === "/api/client/status/" && response.request().method() === "GET";
    });
    const pausedRound = publicPage.getByRole("region", { name: "Текущий раунд" })
      .getByText("Пауза", { exact: true });
    // Another API replica can return its pre-pause, 500 ms cached scoreboard.
    // Deliver that snapshot after the authoritative status has reached the UI.
    await publicPage.route("**/api/client/scoreboard/", async (route) => {
      const response = await route.fetch();
      const body = await response.json() as ScoreboardResponse;
      await resynchronizedRuntime;
      await expect(pausedRound).toBeVisible();
      await route.fulfill({ response, json: {
        ...body,
        runtime: { ...body.runtime, phase: "running", practice: true },
      } });
    }, { times: 1 });
    const phasesBeforeReconnect = observation.phases.length;
    observation.suppressInits(true);
    observation.reconnect();
    await publicContext.setOffline(false);
    const [resyncResponse] = await Promise.all([
      resynchronized,
      expect.poll(() => observation.connections, {
        message: "Expected a new public WebSocket after connectivity returns",
        timeout: 60_000,
      }).toBeGreaterThan(connectionsBeforeOffline),
    ]);
    expect(resyncResponse.status()).toBe(200);
    const resyncBody = await resyncResponse.json() as ScoreboardResponse;
    expect(resyncBody.teams).toHaveLength(2);
    expect(resyncBody.teams.map((team) => team.name)).toEqual(
      expect.arrayContaining(["first", "second"]),
    );
    expect(resyncBody.runtime.phase).toBe("running");
    expect(resyncBody.runtime.practice).toBe(true);
    const runtimeBody = await (await resynchronizedRuntime).json() as ScoreboardResponse["runtime"];
    expect(runtimeBody.phase).toBe("paused");
    expect(runtimeBody.practice).toBe(true);
    await expect.poll(async () =>
      (await firstTaskScore.textContent() ?? "").replace(/\D/g, ""),
    ).not.toBe(String(staleScore));
    await expect(pausedRound).toBeVisible({ timeout: 1000 });
    expect(observation.phases).toHaveLength(phasesBeforeReconnect);
    observation.suppressInits(false);

    const runningBeforeResume = observation.phases.filter((phase) => phase === "running").length;
    await adminPage.getByRole("button", { name: "Продолжить", exact: true }).click();
    await expectNewPhase(observation, "running", runningBeforeResume);
    await expect(adminPage.getByRole("button", {
      name: "Приостановить",
      exact: true,
    })).toBeEnabled();
    await expect(publicPage.getByRole("banner")
      .getByText("Игра идет", { exact: true })).toBeVisible();

    const waitingBeforeFinish = observation.phases.filter((phase) => phase === "waiting").length;
    await adminPage.getByRole("button", { name: "Завершить тестовую игру", exact: true }).click();
    await adminPage.getByRole("dialog").getByRole("button", {
      name: "Завершить тестовую игру",
      exact: true,
    }).click();
    await expectNewPhase(observation, "waiting", waitingBeforeFinish);
    await expect.poll(async () => {
      const status = await currentRuntime(adminPage);
      return { phase: status.phase, practice: status.practice, reset_pending: status.reset_pending };
    }, { timeout: 90_000 }).toEqual({
      phase: "waiting",
      practice: false,
      reset_pending: false,
    });
    await expect(adminPage.getByRole("button", {
      name: "Начать тестовую игру",
      exact: true,
    })).toBeEnabled();
    await expect(publicPage.getByRole("banner")
      .getByText("Ожидание старта", { exact: true })).toBeVisible();
    expect(pageErrors).toEqual([]);
  } finally {
    try {
      if (adminAuthenticated) await resetPractice(adminPage);
    } finally {
      try {
        await adminContext.close();
      } finally {
        await publicContext.close();
      }
    }
  }
});
