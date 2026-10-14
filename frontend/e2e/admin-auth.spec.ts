import { expect, test } from "@playwright/test";
import { createE2EContext, readAdminCredentials } from "./support";

test("admin access, login persistence, and logout use the live session", async ({ browser }) => {
  const context = await createE2EContext(browser);
  const page = await context.newPage();
  const credentials = await readAdminCredentials();

  try {
    await page.goto("/admin/scoreboard");
    await expect(page).toHaveURL(/\/admin\/login$/);
    await expect(page.getByRole("heading", { name: "Вход в управление" })).toBeVisible();

    await page.getByLabel("Логин", { exact: true }).fill(credentials.username);
    await page.getByLabel("Пароль", { exact: true }).fill(`${credentials.password}wrong`);
    await page.getByRole("button", { name: "Войти", exact: true }).click();
    await expect(page.getByRole("alert")).toHaveText("Неверный логин или пароль");
    await expect(page).toHaveURL(/\/admin\/login$/);

    await page.getByLabel("Пароль", { exact: true }).fill(credentials.password);
    await page.getByRole("button", { name: "Войти", exact: true }).click();
    await expect(page).toHaveURL(/\/admin\/scoreboard$/);
    await expect(page.getByRole("heading", { name: "Управление соревнованием" })).toBeVisible();

    const addTeam = page.getByRole("button", { name: "Добавить команду", exact: true });
    const addService = page.getByRole("button", { name: "Добавить сервис", exact: true });
    const startPractice = page.getByRole("button", { name: "Начать тестовую игру", exact: true });
    await expect(startPractice).toBeVisible();
    const teamBox = await addTeam.boundingBox();
    const serviceBox = await addService.boundingBox();
    const startBox = await startPractice.boundingBox();
    expect(teamBox).not.toBeNull();
    expect(serviceBox).not.toBeNull();
    expect(startBox).not.toBeNull();
    expect(serviceBox!.y).toBeGreaterThanOrEqual(teamBox!.y + teamBox!.height);
    expect(teamBox!.height).toBe(startBox!.height);
    await expect(page.locator(".admin-actions .brand-icon")).toHaveCount(0);
    await expect(page.locator("header").getByRole("button", { name: "Выйти", exact: true })).toBeVisible();

    await page.getByRole("button", { name: "English", exact: true }).click();
    await page.getByRole("button", { name: "Add team", exact: true }).click();
    await expect(page).toHaveURL(/\/admin\/team\/create$/);
    await expect(page.getByRole("heading", { name: "Create team", exact: true })).toBeVisible();
    await expect(page.locator("header").getByRole("button", { name: "Sign out", exact: true })).toBeVisible();
    await page.goto("/admin/scoreboard");
    await page.getByRole("button", { name: "Add service", exact: true }).click();
    await expect(page).toHaveURL(/\/admin\/task\/create$/);
    await expect(page.getByRole("heading", { name: "Create service", exact: true })).toBeVisible();
    await page.goto("/admin/scoreboard");
    await page.setViewportSize({ width: 390, height: 844 });
    await expect(page.locator("header").getByRole("button", { name: "Sign out", exact: true })).toBeInViewport();
    await expect(page.getByRole("button", { name: "English", exact: true })).toBeInViewport();
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
    await page.setViewportSize({ width: 1280, height: 720 });
    await page.getByRole("button", { name: "Русский", exact: true }).click();

    const sessionAfterReload = page.waitForResponse((response) => {
      const url = new URL(response.url());
      return url.pathname === "/api/admin/status/" && response.request().method() === "GET";
    });
    await page.reload();
    expect((await sessionAfterReload).status()).toBe(200);
    await expect(page.getByRole("heading", { name: "Управление соревнованием" })).toBeVisible();

    await page.getByRole("button", { name: "Выйти", exact: true }).click();
    await expect(page).toHaveURL(/\/admin\/login$/);

    const invalidatedSession = page.waitForResponse((response) => {
      const url = new URL(response.url());
      return url.pathname === "/api/admin/status/" && response.request().method() === "GET";
    });
    await page.goto("/admin/task/create");
    expect((await invalidatedSession).status()).toBe(403);
    await expect(page).toHaveURL(/\/admin\/login$/);
  } finally {
    await context.close();
  }
});
