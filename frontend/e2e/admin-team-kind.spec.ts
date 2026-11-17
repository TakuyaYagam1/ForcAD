import { expect, test } from "@playwright/test";
import { createE2EContext, loginAsAdmin, readAdminCredentials } from "./support";

test("creating an NPC exposes its IP but locks generated team details", async ({ browser }) => {
  const context = await createE2EContext(browser);
  const page = await context.newPage();

  try {
    await loginAsAdmin(page, await readAdminCredentials());
    await page.getByRole("button", {
      name: "Добавить команду"
    }).click();
    const kind = page.getByLabel("Тип команды");
    await expect(kind.locator('option[value="player"]')).toHaveText("Команда");
    const name = page.getByLabel("Название команды", {
      exact: true
    });
    const ip = page.getByLabel("IP-адрес", {
      exact: true
    });
    const logo = page.getByLabel("Ссылка на логотип");

    await name.fill("Team draft");
    await logo.fill("/team-logo.png");
    await kind.selectOption("npc");
    await expect(name).toHaveValue("NPC");
    await expect(name).toBeDisabled();
    await expect(ip).toBeEditable();
    await expect(logo).toBeDisabled();
    await expect(page.getByLabel("Выделить в рейтинге")).toBeDisabled();
    await expect(page.getByLabel("Активна")).toBeDisabled();

    await kind.selectOption("player");
    await expect(name).toHaveValue("Team draft");
    await expect(name).toBeEditable();
    await expect(logo).toHaveValue("/team-logo.png");
    await expect(logo).toBeEditable();
    await page.getByRole("button", {
      name: "English",
      exact: true
    }).click();
    await expect(page.getByLabel("Team kind").locator('option[value="player"]')).toHaveText("Team");
  } finally {
    await context.close();
  }
});

test("admin actions align to the same column edges", async ({ browser }) => {
  const context = await createE2EContext(browser);
  const page = await context.newPage();

  try {
    await page.setViewportSize({
      width: 1568,
      height: 980
    });
    await loginAsAdmin(page, await readAdminCredentials());
    const addTeam = await page.getByRole("button", {
      name: "Добавить команду"
    }).boundingBox();
    const addService = await page.getByRole("button", {
      name: "Добавить сервис"
    }).boundingBox();
    const settings = await page.getByTestId("game-settings-trigger").boundingBox();
    const pause = await page.getByRole("button", {
      name: "Приостановить"
    }).boundingBox();
    if (!addTeam || !addService || !settings || !pause) {
      throw new Error("Admin actions are missing");
    }
    expect(Math.abs(addTeam.x - addService.x)).toBeLessThan(2);
    expect(Math.abs(addTeam.x - settings.x)).toBeLessThan(2);
    expect(Math.abs(addTeam.x - pause.x)).toBeLessThan(2);
    expect(Math.abs(addTeam.width - settings.width)).toBeLessThan(2);
  } finally {
    await context.close();
  }
});
