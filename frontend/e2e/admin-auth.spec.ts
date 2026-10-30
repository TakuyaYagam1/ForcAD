import { expect, test } from "@playwright/test";
import { createE2EContext, expectDocumentSecurity, observeCSP, readAdminCredentials } from "./support";

test("admin access, login persistence, and logout use the live session", async ({ browser }) => {
  const context = await createE2EContext(browser);
  const page = await context.newPage();
  const credentials = await readAdminCredentials();
  const cspViolations = await observeCSP(page);

  try {
    expectDocumentSecurity(await page.goto("/admin/scoreboard"));
    await expect(page).toHaveURL(/\/admin\/login$/);
    await expect(page.getByRole("heading", { name: "Вход в управление" })).toBeVisible();

    await page.getByLabel("Логин", { exact: true }).fill(credentials.username);
    await page.getByLabel("Пароль", { exact: true }).fill(`${credentials.password}wrong`);
    await page.getByRole("button", { name: "Войти", exact: true }).click();
    await expect(page.getByRole("alert")).toHaveText("Неверный логин или пароль");
    await expect(page).toHaveURL(/\/admin\/login$/);

    await page.getByLabel("Пароль", { exact: true }).fill(credentials.password);
    const loginResponse = page.waitForResponse((response) =>
      new URL(response.url()).pathname === "/api/admin/login/"
      && response.request().method() === "POST");
    await page.getByRole("button", { name: "Войти", exact: true }).click();
    const login = await loginResponse;
    expect(login.status()).toBe(200);
    const csrfToken = (await login.json() as { csrf_token?: unknown }).csrf_token;
    expect(typeof csrfToken === "string" && /^[0-9a-f]{64}$/.test(csrfToken)).toBe(true);
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
    const session = await sessionAfterReload;
    expect(session.status()).toBe(200);
    expect((await session.json() as { csrf_token?: unknown }).csrf_token === csrfToken).toBe(true);
    await expect(page.getByRole("heading", { name: "Управление соревнованием" })).toBeVisible();

    const rejected = await page.evaluate(async () => {
      const status = await fetch("/api/admin/status/", { cache: "no-store" });
      const { csrf_token: token } = await status.json() as { csrf_token: string };
      const wrong = (token[0] === "0" ? "1" : "0") + token.slice(1);
      const statuses: number[] = [];
      const headerCases: Record<string, string>[] = [
        { "Content-Type": "application/json" },
        { "Content-Type": "application/json", "X-CSRF-Token": wrong },
      ];
      for (const headers of headerCases) {
        const response = await fetch("/api/admin/logout/", {
          method: "POST", headers, body: "{}",
        });
        statuses.push(response.status);
      }
      statuses.push((await fetch("/api/admin/status/", { cache: "no-store" })).status);
      return statuses;
    });
    expect(rejected).toEqual([403, 403, 200]);
    const logoutResponse = page.waitForResponse((response) =>
      new URL(response.url()).pathname === "/api/admin/logout/"
      && response.request().method() === "POST");
    await page.getByRole("button", { name: "Выйти", exact: true }).click();
    const logout = await logoutResponse;
    expect(logout.status()).toBe(200);
    expect((await logout.request().headerValue("X-CSRF-Token")) === csrfToken).toBe(true);
    await expect(page).toHaveURL(/\/admin\/login$/);

    const invalidatedSession = page.waitForResponse((response) => {
      const url = new URL(response.url());
      return url.pathname === "/api/admin/status/" && response.request().method() === "GET";
    });
    await page.goto("/admin/task/create");
    expect((await invalidatedSession).status()).toBe(403);
    await expect(page).toHaveURL(/\/admin\/login$/);
    expect(cspViolations).toEqual([]);
  } finally {
    await context.close();
  }
});
