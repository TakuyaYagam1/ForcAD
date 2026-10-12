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
