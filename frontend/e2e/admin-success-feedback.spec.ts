import { expect, test } from "@playwright/test";
import { createE2EContext, loginAsAdmin, readAdminCredentials } from "./support";

const teamId = "0199f7c0-0000-7000-8000-000000000051";
const taskId = "0199f7c0-0000-7000-8000-000000000052";

test("team creation and edits show distinct confirmation after navigation", async ({ browser }) => {
  const context = await createE2EContext(browser);
  const page = await context.newPage();
  let team: Record<string, unknown> | null = null;
  try {
    await loginAsAdmin(page, await readAdminCredentials());
    await page.route("**/api/v1/admin/teams/", async (route) => {
      if (route.request().method() !== "POST") {
        await route.continue();
        return;
      }
      team = {
        ...route.request().postDataJSON(),
        id: teamId,
        token: "0123456789abcdef"
      };
      await route.fulfill({
        json: team
      });
    });
    await page.route(`**/api/v1/admin/teams/${teamId}/`, async (route) => {
      if (route.request().method() === "PUT") {
        team = {
          ...route.request().postDataJSON(),
          id: teamId
        };
      }
      await route.fulfill({
        json: team
      });
    });
    await page.goto("/admin/team/create");
    await page.getByLabel("Название команды", {
      exact: true
    }).fill("Team test");
    await page.getByLabel("IP-адрес", {
      exact: true
    }).fill("192.0.2.51");
    await page.getByRole("button", {
      name: "Сохранить"
    }).click();
    await expect(page).toHaveURL(new RegExp(`/admin/team/${teamId}$`));
    await expect(page.getByTestId("admin-notification")).toContainText("Команда добавлена");
    for (const width of [1568, 720]) {
      await page.setViewportSize({
        width,
        height: 900
      });
      await expect.poll(async () => {
        const header = await page.locator(".site-header").boundingBox();
        const notification = await page.getByTestId("admin-notification").boundingBox();
        return header !== null
          && notification !== null
          && notification.y >= header.y + header.height + 8;
      }).toBe(true);
    }
    await page.setViewportSize({
      width: 1280,
      height: 720
    });

    await page.getByLabel("Название команды", {
      exact: true
    }).fill("Team test edited");
    await page.getByRole("button", {
      name: "Сохранить"
    }).click();
    await expect(page.getByTestId("admin-notification")).toContainText("Команда сохранена");
    await page.getByLabel("Активна").uncheck();
    await page.getByRole("button", {
      name: "Сохранить"
    }).click();
    await expect(page.getByTestId("admin-notification")).toContainText("Команда отключена");
  } finally {
    await context.close();
  }
});

test("service creation and edits show distinct confirmation after navigation", async ({ browser }) => {
  const context = await createE2EContext(browser);
  const page = await context.newPage();
  let task: Record<string, unknown> | null = null;
  try {
    await loginAsAdmin(page, await readAdminCredentials());
    await page.route("**/api/v1/admin/tasks/", async (route) => {
      if (route.request().method() !== "POST") {
        await route.continue();
        return;
      }
      task = {
        ...route.request().postDataJSON(),
        id: taskId
      };
      await route.fulfill({
        json: task
      });
    });
    await page.route(`**/api/v1/admin/tasks/${taskId}/`, async (route) => {
      if (route.request().method() === "PUT") {
        task = {
          ...route.request().postDataJSON(),
          id: taskId
        };
      }
      await route.fulfill({
        json: task
      });
    });
    await page.goto("/admin/task/create");
    await page.getByLabel("Название сервиса", {
      exact: true
    }).fill("Service test");
    await page.getByLabel("Путь к чекеру", {
      exact: true
    }).fill("example/checker.py");
    await page.getByRole("button", {
      name: "Сохранить"
    }).click();
    await expect(page).toHaveURL(new RegExp(`/admin/task/${taskId}$`));
    await expect(page.getByTestId("admin-notification")).toContainText("Сервис добавлен");

    await page.getByLabel("Название сервиса", {
      exact: true
    }).fill("Service test edited");
    await page.getByRole("button", {
      name: "Сохранить"
    }).click();
    await expect(page.getByTestId("admin-notification")).toContainText("Сервис сохранён");
    await page.getByLabel("Сервис активен").uncheck();
    await page.getByRole("button", {
      name: "Сохранить"
    }).click();
    await expect(page.getByTestId("admin-notification")).toContainText("Сервис отключён");
  } finally {
    await context.close();
  }
});
