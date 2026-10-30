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
    await expect(page.getByRole("region", { name: "Топ команд" })).toBeVisible();

    const teamLinks = page.locator('a[href^="/team/"]');
    await expect(teamLinks).toHaveCount(2);
    const firstTeam = teamLinks.filter({ hasText: "first" });
    const secondTeam = teamLinks.filter({ hasText: "second" });
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
    await expect(page.getByRole("heading", { name: "first", exact: true })).toBeVisible();
    await expect(page.getByRole("heading", { name: "История раундов", exact: true })).toBeVisible();
    await expect(page.getByRole("region", { name: "История раундов команды" })).toBeVisible();
    await expect(page.getByRole("table", { name: "История результатов команды по раундам" })).toBeVisible();

    const directReloadHistory = page.waitForResponse((response) => {
      const url = new URL(response.url());
      return url.pathname === `/api/client/teams/${teamId}/` && response.request().method() === "GET";
    });
    await page.reload();
    expect((await directReloadHistory).status()).toBe(200);
    await expect(page.getByRole("heading", { name: "first", exact: true })).toBeVisible();

    const rootScoreboard = page.waitForResponse((response) => {
      const url = new URL(response.url());
      return url.pathname === "/api/client/scoreboard/" && response.request().method() === "GET";
    });
    await page.goto("/");
    expect((await rootScoreboard).status()).toBe(200);
    await expect(page.getByRole("heading", { name: "Рейтинг команд", exact: true })).toBeVisible();
    expect(cspViolations).toEqual([]);
  } finally {
    await context.close();
  }
});
