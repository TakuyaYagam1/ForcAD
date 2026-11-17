import { env } from "node:process";
import { expect, test } from "@playwright/test";
import { createE2EContext, loginAsAdmin, readAdminCredentials } from "./support";

test("admin sections lead to team and service editors", async ({ browser }) => {
  const context = await createE2EContext(browser);
  const page = await context.newPage();
  const credentials = env.FORCAD_E2E_DEMO === "1"
    ? {
      username: "demo",
      password: "demo"
    }
    : await readAdminCredentials();

  try {
    await loginAsAdmin(page, credentials);
    const navigation = page.getByRole("navigation", {
      name: "Разделы админки"
    });
    const teams = navigation.getByRole("link", {
      name: "Команды"
    });
    const game = navigation.getByRole("link", {
      name: "Игра"
    });
    const services = navigation.getByRole("link", {
      name: "Сервисы"
    });

    await expect(navigation.getByRole("link")).toHaveText(["Команды", "Игра", "Сервисы"]);
    await expect(game).toHaveAttribute("aria-current", "page");

    await teams.click();
    await expect(page).toHaveURL(/\/admin\/teams$/);
    await expect(page.getByRole("heading", {
      name: "Все команды"
    })).toBeVisible();
    await expect(teams).toHaveAttribute("aria-current", "page");
    await page.getByRole("list").getByRole("link").first().click();
    await expect(page).toHaveURL(/\/admin\/team\/[0-9a-f-]+$/);
    await expect(teams).toHaveAttribute("aria-current", "page");

    await services.click();
    await expect(page).toHaveURL(/\/admin\/services$/);
    await expect(page.getByRole("heading", {
      name: "Все сервисы"
    })).toBeVisible();
    await expect(services).toHaveAttribute("aria-current", "page");
    await page.getByRole("list").getByRole("link").first().click();
    await expect(page).toHaveURL(/\/admin\/task\/[0-9a-f-]+$/);
    await expect(services).toHaveAttribute("aria-current", "page");

    await game.click();
    await expect(page).toHaveURL(/\/admin\/scoreboard$/);
    await expect(game).toHaveAttribute("aria-current", "page");
  } finally {
    await context.close();
  }
});
