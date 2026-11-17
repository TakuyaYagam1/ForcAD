import { expect, test } from "@playwright/test";
import { createE2EContext, loginAsAdmin, readAdminCredentials } from "./support";

const teamIds = [
  "0199f7c0-0000-7000-8000-000000000031",
  "0199f7c0-0000-7000-8000-000000000032",
];
const taskId = "0199f7c0-0000-7000-8000-000000000041";

test("admin can reactivate inactive teams and services in confirmed batches", async ({ browser }) => {
  const context = await createE2EContext(browser);
  const page = await context.newPage();
  const teams = teamIds.map((id, index) => {
    return {
      id,
      name: `Inactive team ${index + 1}`,
      ip: `10.0.0.${index + 31}`,
      token: "0123456789abcdef",
      kind: "player",
      highlighted: false,
      active: false,
      logo_path: "",
    };
  });
  const services = [{
    id: taskId,
    name: "Inactive service",
    checker: "check.py",
    gets: 1,
    puts: 1,
    places: 1,
    checker_timeout: 20,
    checker_type: "hackerdom",
    env_path: "",
    default_score: 2500,
    get_period: 10,
    active: false,
  }];
  const batches: Array<{
    path: string;
    ids: string[];
  }> = [];

  try {
    await loginAsAdmin(page, await readAdminCredentials());
    await page.route("**/api/v1/admin/teams/", async (route) => {
      await route.fulfill({
        json: teams
      });
    });
    await page.route("**/api/v1/admin/tasks/", async (route) => {
      await route.fulfill({
        json: services
      });
    });
    for (const [path, items] of [
      ["teams", teams],
      ["tasks", services],
    ] as const) {
      await page.route(`**/api/v1/admin/${path}/activate/`, async (route) => {
        const { ids } = route.request().postDataJSON() as {
          ids: string[]
        };
        batches.push({
          path,
          ids,
        });
        for (const item of items) {
          if (ids.includes(item.id)) {
            item.active = true;
          }
        }
        await route.fulfill({
          json: {
            activated: ids.length
          }
        });
      });
    }

    await page.goto("/admin/teams");
    await page.getByRole("checkbox", {
      name: "Выбрать неактивных (2)"
    }).click();
    await page.getByRole("button", {
      name: "Включить выбранные (2)"
    }).click();
    const confirmTeams = page.getByRole("dialog");
    await expect(confirmTeams).toBeVisible();
    await confirmTeams.getByRole("button", {
      name: "Включить (2)"
    }).click();
    await expect(page.getByTestId("admin-notification")).toContainText("2");
    await expect(page.getByRole("listitem").filter({
      hasText: "Inactive team 1"
    }).getByRole("checkbox")).toBeVisible();
    await expect(page.getByRole("listitem").filter({
      hasText: "Inactive team 2"
    }).getByRole("checkbox")).toBeVisible();

    await page.goto("/admin/services");
    await page.getByRole("checkbox", {
      name: "Выбрать неактивных (1)"
    }).click();
    await page.getByRole("button", {
      name: "Включить выбранные (1)"
    }).click();
    await page.getByRole("dialog").getByRole("button", {
      name: "Включить (1)"
    }).click();
    await expect(page.getByTestId("admin-notification")).toContainText("1");
    expect(batches).toEqual([
      {
        path: "teams",
        ids: teamIds,
      },
      {
        path: "tasks",
        ids: [taskId],
      },
    ]);
  } finally {
    await context.close();
  }
});
