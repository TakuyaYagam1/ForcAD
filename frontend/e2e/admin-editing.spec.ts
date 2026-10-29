import { expect, test } from "@playwright/test";
import { createE2EContext, loginAsAdmin, readAdminCredentials } from "./support";

test("admin validates and restores team and service edits", async ({ browser }) => {
  const context = await createE2EContext(browser);
  const page = await context.newPage();
  const credentials = await readAdminCredentials();

  try {
    await loginAsAdmin(page, credentials);
    const teamButton = page.locator(".scoreboard-row").filter({ hasText: "first" }).locator(".team-identity");
    await expect(teamButton).toBeVisible();
    await teamButton.click();
    const teamId = /^\/admin\/team\/([0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12})$/i.exec(new URL(page.url()).pathname)?.[1];
    await page.goto("/admin/scoreboard");
    const taskButton = page.getByRole("columnheader", { name: "example", exact: true }).getByRole("button");
    await expect(taskButton).toBeVisible();
    await taskButton.click();
    const taskId = /^\/admin\/task\/([0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12})$/i.exec(new URL(page.url()).pathname)?.[1];
    expect(teamId).toBeDefined();
    expect(taskId).toBeDefined();

    const teamNameInput = page.getByLabel("Название команды", { exact: true });
    const teamIPInput = page.getByLabel("IP-адрес", { exact: true });
    const originalTeamName = "first";
    const editedTeamName = `${originalTeamName} e2e`;
    let originalTeamIP = "";

    await page.goto(`/admin/team/${teamId}`);
    await expect(teamNameInput).toHaveValue(originalTeamName);
    originalTeamIP = await teamIPInput.inputValue();
    await teamIPInput.fill("invalid-ip");
    await page.getByRole("button", { name: "Сохранить", exact: true }).click();
    await expect(page.getByRole("alert")).toHaveText("Укажите корректный IPv4 или IPv6 адрес.");

    try {
      await teamIPInput.fill(originalTeamIP);
      await teamNameInput.fill(editedTeamName);
      const teamUpdate = page.waitForResponse((response) => {
        const url = new URL(response.url());
        return url.pathname === `/api/admin/teams/${teamId}/` && response.request().method() === "PUT";
      });
      await page.getByRole("button", { name: "Сохранить", exact: true }).click();
      expect((await teamUpdate).status()).toBe(200);

      await page.goto("/teams");
      await expect(page.getByRole("heading", { name: editedTeamName, exact: true })).toBeVisible();
    } finally {
      await page.goto(`/admin/team/${teamId}`);
      await expect(teamNameInput).toBeVisible();
      await teamNameInput.fill(originalTeamName);
      await teamIPInput.fill(originalTeamIP);
      const teamRestore = page.waitForResponse((response) => {
        const url = new URL(response.url());
        return url.pathname === `/api/admin/teams/${teamId}/` && response.request().method() === "PUT";
      });
      await page.getByRole("button", { name: "Сохранить", exact: true }).click();
      expect((await teamRestore).status()).toBe(200);
    }

    const taskNameInput = page.getByLabel("Название сервиса", { exact: true });
    const originalTaskName = "example";
    const editedTaskName = `${originalTaskName} e2e`;
    await page.goto(`/admin/task/${taskId}`);
    await expect(taskNameInput).toHaveValue(originalTaskName);
    await taskNameInput.fill("   ");
    await page.getByRole("button", { name: "Сохранить", exact: true }).click();
    await expect(page.getByRole("alert")).toContainText("Название и путь чекера должны быть заполнены");

    try {
      await taskNameInput.fill(editedTaskName);
      const taskUpdate = page.waitForResponse((response) => {
        const url = new URL(response.url());
        return url.pathname === `/api/admin/tasks/${taskId}/` && response.request().method() === "PUT";
      });
      await page.getByRole("button", { name: "Сохранить", exact: true }).click();
      expect((await taskUpdate).status()).toBe(200);

      await page.goto(`/team/${teamId}`);
      const historyTable = page.getByRole("table", {
        name: "История результатов команды по раундам",
      });
      await expect(historyTable.getByRole("columnheader", {
        name: editedTaskName,
        exact: true,
      })).toBeVisible();
    } finally {
      await page.goto(`/admin/task/${taskId}`);
      await expect(taskNameInput).toBeVisible();
      await taskNameInput.fill(originalTaskName);
      const taskRestore = page.waitForResponse((response) => {
        const url = new URL(response.url());
        return url.pathname === `/api/admin/tasks/${taskId}/` && response.request().method() === "PUT";
      });
      await page.getByRole("button", { name: "Сохранить", exact: true }).click();
      expect((await taskRestore).status()).toBe(200);
    }

    await page.goto("/teams");
    await expect(page.getByRole("heading", { name: originalTeamName, exact: true })).toBeVisible();
    await page.goto(`/team/${teamId}`);
    const historyTable = page.getByRole("table", {
      name: "История результатов команды по раундам",
    });
    await expect(historyTable.getByRole("columnheader", {
      name: originalTaskName,
      exact: true,
    })).toBeVisible();
  } finally {
    await context.close();
  }
});
