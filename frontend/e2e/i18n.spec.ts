import { expect, test } from "@playwright/test";
import { createE2EContext, observePublicStream } from "./support";

test("language switching preserves the team list, survives reload and translates validation", async ({ browser }) => {
  const context = await createE2EContext(browser);
  const page = await context.newPage();
  const stream = await observePublicStream(page);

  try {
    await page.goto("/teams");
    await expect(page.getByRole("region", {
      name: "Топ команд"
    })).toBeVisible();
    await expect(page.locator('a[href^="/team/"]').filter({
      hasText: "first"
    })).toBeVisible();
    await expect.poll(() => {
      return stream.connections
    }).toBeGreaterThan(0);
    const connections = stream.connections;
    await page.getByRole("button", {
      name: "English",
      exact: true
    }).click();
    await expect(page.getByRole("region", {
      name: "Top teams"
    })).toBeVisible();
    await expect(page.locator('a[href^="/team/"]').filter({
      hasText: "first"
    })).toBeVisible();
    await expect(page.locator("html")).toHaveAttribute("lang", "en");
    await expect(page.getByRole("button", {
      name: "English",
      exact: true
    })).toHaveAttribute("aria-pressed", "true");
    expect(stream.connections).toBe(connections);
    await page.setViewportSize({
      width: 390,
      height: 844
    });
    await expect(page.getByRole("button", {
      name: "English",
      exact: true
    })).toBeInViewport();
    await expect(page.getByRole("navigation", {
      name: "Main navigation"
    })).toBeInViewport();
    expect(await page.evaluate(() => {
      return document.documentElement.scrollWidth <= window.innerWidth
    })).toBe(true);

    await page.reload();
    await expect(page.getByRole("region", {
      name: "Top teams"
    })).toBeVisible();
    await expect(page).toHaveTitle("ForcAD - team standings");
    await page.goto("/admin/login");
    await expect(page.getByRole("heading", {
      name: "Admin sign in",
      exact: true
    })).toBeVisible();
    await page.getByRole("button", {
      name: "Sign in",
      exact: true
    }).click();
    await expect(page.getByText("Enter a username", {
      exact: true
    })).toBeVisible();
    await expect(page.getByText("Enter a password", {
      exact: true
    })).toBeVisible();

    await page.getByRole("button", {
      name: "Русский",
      exact: true
    }).click();
    await expect(page.getByRole("heading", {
      name: "Вход в управление",
      exact: true
    })).toBeVisible();
    await expect(page.getByText("Введите логин", {
      exact: true
    })).toBeVisible();
    await expect(page.getByText("Введите пароль", {
      exact: true
    })).toBeVisible();
    await expect(page.locator("html")).toHaveAttribute("lang", "ru");
  } finally {
    await context.close();
  }
});

test("demo checker messages stay fixed while labels and timestamps change language", async ({ page }) => {
  await page.goto("/?demo=1");
  const round = page.locator(".header-round");
  const initialRound = Number((await round.innerText()).match(/\d+/u)?.[0]);
  const currentRound = initialRound + 1;
  await page.getByRole("button", {
    name: "Новое событие",
  }).click();
  await expect(round).toHaveText(`Раунд ${currentRound}`);
  await page.locator(".team-identity").filter({
    hasText: "Null Route",
  }).click();

  const eventTime = page.locator(".team-content-aside time").first();
  await expect(eventTime).toBeVisible();
  const russianTime = await eventTime.innerText();
  const eventTimestamp = await eventTime.getAttribute("datetime");
  expect(eventTimestamp).not.toBeNull();
  const service = page.locator(".services-grid").getByRole("button", {
    name: /^Market:/,
  });
  const dialog = page.getByRole("dialog");
  await service.click();
  const historyTable = dialog.getByRole("table");
  await expect(historyTable.getByRole("columnheader", {
    name: "Ответ чекера",
    exact: true,
  })).toBeVisible();
  const errorRow = historyTable.getByRole("row").filter({
    hasText: "Service unavailable: connection refused",
  }).first();
  const okRow = historyTable.getByRole("row").filter({
    hasText: "OK",
  }).first();
  await expect(errorRow.getByRole("cell", {
    name: "Service unavailable: connection refused",
    exact: true,
  })).toBeVisible();
  await expect(okRow.getByRole("cell", {
    name: "OK",
    exact: true,
  })).toBeVisible();
  await expect(historyTable).not.toContainText("Сервис недоступен");
  await page.keyboard.press("Escape");

  await page.getByRole("button", {
    name: "English",
    exact: true,
  }).click();
  await expect(round).toHaveText(`Round ${currentRound}`);
  await expect(eventTime).not.toHaveText(russianTime);
  await expect(eventTime).toHaveAttribute("datetime", eventTimestamp ?? "");
  await service.click();
  const englishHistoryTable = dialog.getByRole("table");
  await expect(englishHistoryTable.getByRole("columnheader", {
    name: "Checker response",
    exact: true,
  })).toBeVisible();
  const englishErrorRow = englishHistoryTable.getByRole("row").filter({
    hasText: "Service unavailable: connection refused",
  }).first();
  const englishOkRow = englishHistoryTable.getByRole("row").filter({
    hasText: "OK",
  }).first();
  await expect(englishErrorRow.getByRole("cell", {
    name: "Service unavailable: connection refused",
    exact: true,
  })).toBeVisible();
  await expect(englishOkRow.getByRole("cell", {
    name: "OK",
    exact: true,
  })).toBeVisible();
  await expect(englishHistoryTable).not.toContainText("Сервис недоступен");
});

test("unsupported saved language falls back to Russian", async ({ browser }) => {
  const context = await createE2EContext(browser);
  await context.addInitScript(() => {
    return localStorage.setItem("forcad-language", "de")
  });
  const page = await context.newPage();
  try {
    await page.goto("/");
    await expect(page.getByRole("heading", {
      name: "Рейтинг команд",
      exact: true
    })).toBeVisible();
    await expect(page.locator("html")).toHaveAttribute("lang", "ru");
  } finally {
    await context.close();
  }
});
