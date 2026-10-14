import { expect, test } from "@playwright/test";
import { createE2EContext, observePublicStream } from "./support";

test("language switching preserves page state, survives reload and translates validation", async ({ browser }) => {
  const context = await createE2EContext(browser);
  const page = await context.newPage();
  const stream = await observePublicStream(page);

  try {
    await page.goto("/teams");
    await expect(page.getByRole("heading", { name: "Команды", exact: true })).toBeVisible();
    const search = page.getByRole("searchbox");
    await search.fill("first");
    await expect.poll(() => stream.connections).toBeGreaterThan(0);
    const connections = stream.connections;
    await page.getByRole("button", { name: "English", exact: true }).click();
    await expect(page.getByRole("heading", { name: "Teams", exact: true })).toBeVisible();
    await expect(search).toHaveValue("first");
    await expect(page.locator("html")).toHaveAttribute("lang", "en");
    await expect(page.getByRole("button", { name: "English", exact: true })).toHaveAttribute("aria-pressed", "true");
    expect(stream.connections).toBe(connections);
    await page.setViewportSize({ width: 390, height: 844 });
    await expect(page.getByRole("button", { name: "English", exact: true })).toBeInViewport();
    await expect(page.getByRole("navigation", { name: "Main navigation" })).toBeInViewport();
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);

    await page.reload();
    await expect(page.getByRole("heading", { name: "Teams", exact: true })).toBeVisible();
    await expect(page).toHaveTitle("ForcAD - team standings");
    await page.goto("/admin/login");
    await expect(page.getByRole("heading", { name: "Admin sign in", exact: true })).toBeVisible();
    await page.getByRole("button", { name: "Sign in", exact: true }).click();
    await expect(page.getByText("Enter a username", { exact: true })).toBeVisible();
    await expect(page.getByText("Enter a password", { exact: true })).toBeVisible();

    await page.getByRole("button", { name: "Русский", exact: true }).click();
    await expect(page.getByRole("heading", { name: "Вход в управление", exact: true })).toBeVisible();
    await expect(page.getByText("Введите логин", { exact: true })).toBeVisible();
    await expect(page.getByText("Введите пароль", { exact: true })).toBeVisible();
    await expect(page.locator("html")).toHaveAttribute("lang", "ru");
  } finally {
    await context.close();
  }
});

test("unsupported saved language falls back to Russian", async ({ browser }) => {
  const context = await createE2EContext(browser);
  await context.addInitScript(() => localStorage.setItem("forcad-language", "de"));
  const page = await context.newPage();
  try {
    await page.goto("/");
    await expect(page.getByRole("heading", { name: "Рейтинг команд", exact: true })).toBeVisible();
    await expect(page.locator("html")).toHaveAttribute("lang", "ru");
  } finally {
    await context.close();
  }
});
