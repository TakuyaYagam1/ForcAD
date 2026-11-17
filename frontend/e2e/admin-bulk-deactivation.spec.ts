import { env } from "node:process";
import { expect, test } from "@playwright/test";
import { createE2EContext, loginAsAdmin, readAdminCredentials } from "./support";

const alphaId = "0199f7c0-0000-7000-8000-000000000031";
const bravoId = "0199f7c0-0000-7000-8000-000000000032";
const inactiveId = "0199f7c0-0000-7000-8000-000000000033";

const teams = [
  {
    id: alphaId,
    name: "Batch Alpha",
    ip: "10.0.0.31",
    token: "0123456789abcdef",
    kind: "player",
    highlighted: false,
    active: true,
    logo_path: "",
  },
  {
    id: bravoId,
    name: "Batch Bravo",
    ip: "10.0.0.32",
    token: "0123456789abcdef",
    kind: "player",
    highlighted: false,
    active: true,
    logo_path: "",
  },
  {
    id: inactiveId,
    name: "Batch Inactive",
    ip: "10.0.0.33",
    token: "0123456789abcdef",
    kind: "player",
    highlighted: false,
    active: false,
    logo_path: "",
  },
];

const tasks = [
  {
    id: "0199f7c0-0000-7000-8000-000000000041",
    name: "Batch Service",
    checker: "check.py",
    gets: 1,
    puts: 1,
    places: 1,
    checker_timeout: 20,
    checker_type: "hackerdom",
    env_path: "",
    default_score: 2500,
    get_period: 10,
    active: true,
  },
];

test("admin explicitly confirms one batch request and keeps disabled teams editable", async ({ browser }) => {
  const context = await createE2EContext(browser);
  const page = await context.newPage();
  const credentials = env.FORCAD_E2E_DEMO === "1"
    ? {
      username: "demo",
      password: "demo"
    }
    : await readAdminCredentials();
  const postedBatches: string[][] = [];
  const demo = env.FORCAD_E2E_DEMO === "1";
  const firstName = demo ? "Northstar" : "Batch Alpha";
  const secondName = demo ? "Byteforge" : "Batch Bravo";
  const serviceName = demo ? "Vault" : "Batch Service";
  let scoreboardRefreshes = 0;

  page.on("request", (request) => {
    if (new URL(request.url()).pathname === "/api/v1/client/scoreboard/") {
      scoreboardRefreshes += 1;
    }
  });

  try {
    await loginAsAdmin(page, credentials);

    if (!demo) {
      await page.route("**/api/v1/admin/teams/", async (route) => {
        if (route.request().method() !== "GET") {
          await route.continue();
          return;
        }
        await route.fulfill({
          contentType: "application/json",
          body: JSON.stringify(teams),
        });
      });
      await page.route("**/api/v1/admin/tasks/", async (route) => {
        if (route.request().method() !== "GET") {
          await route.continue();
          return;
        }
        await route.fulfill({
          contentType: "application/json",
          body: JSON.stringify(tasks),
        });
      });
      await page.route("**/api/v1/admin/teams/deactivate/", async (route) => {
        const body = route.request().postDataJSON() as {
          ids: string[]
        };
        postedBatches.push(body.ids);
        for (const team of teams) {
          if (body.ids.includes(team.id)) {
            team.active = false;
          }
        }
        await route.fulfill({
          contentType: "application/json",
          body: JSON.stringify({
            deactivated: body.ids.length
          }),
        });
      });
    }
    await page.goto("/admin/teams");
    const alphaRow = page.getByRole("listitem").filter({
      hasText: firstName
    });
    const bravoRow = page.getByRole("listitem").filter({
      hasText: secondName
    });
    const inactiveRow = page.getByRole("listitem").filter({
      hasText: "Batch Inactive"
    });

    await expect(alphaRow.getByRole("checkbox")).toHaveCount(1);
    await expect(bravoRow.getByRole("checkbox")).toHaveCount(1);
    if (!demo) {
      await expect(inactiveRow.getByRole("checkbox")).toBeVisible();
      await expect(inactiveRow.getByRole("link", {
        name: "Batch Inactive"
      })).toBeVisible();
    }

    const selectionControls = page.getByRole("group", {
      name: "Массовые действия"
    });
    await alphaRow.getByRole("checkbox").click();
    await expect(selectionControls.getByRole("button", {
      name: /1/
    })).toBeEnabled();

    await page.goto("/admin/services");
    const serviceRow = page.getByRole("listitem").filter({
      hasText: serviceName
    });
    await expect(serviceRow.getByRole("checkbox")).not.toBeChecked();
    await expect(selectionControls.getByRole("button", {
      name: /^Отключить выбранные/
    })).toBeDisabled();

    await page.goto("/admin/teams");
    await expect(alphaRow.getByRole("checkbox")).not.toBeChecked();
    scoreboardRefreshes = 0;
    if (demo) {
      await alphaRow.getByRole("checkbox").click();
      await bravoRow.getByRole("checkbox").click();
    } else {
      await selectionControls.getByRole("checkbox", {
        name: /^Выбрать активных/
      }).click();
    }
    const batchButton = selectionControls.getByRole("button", {
      name: /2/
    });
    await expect(batchButton).toBeEnabled();
    await batchButton.click();

    const dialog = page.getByRole("dialog");
    await expect(dialog).toBeVisible();
    await dialog.getByRole("button", {
      name: /2/
    }).click();

    if (!demo) {
      await expect.poll(() => {
        return postedBatches
      }).toEqual([[alphaId, bravoId]]);
      await expect.poll(() => {
        return scoreboardRefreshes
      }).toBeGreaterThan(0);
    }
    await expect(alphaRow.getByRole("checkbox")).not.toBeChecked();
    await expect(bravoRow.getByRole("checkbox")).not.toBeChecked();
    await expect(alphaRow.getByRole("link", {
      name: firstName
    })).toBeVisible();
    await expect(selectionControls.getByRole("button", {
      name: /^Отключить выбранные/
    })).toBeDisabled();
    if (demo) {
      await page.goto("/admin/services");
      await serviceRow.getByRole("checkbox").click();
      await selectionControls.getByRole("button", {
        name: /^Отключить выбранные/
      }).click();
      await page.getByRole("dialog").getByRole("button", {
        name: /1/
      }).click();
      await expect(serviceRow.getByRole("checkbox")).not.toBeChecked();
      await expect(serviceRow.getByRole("link", {
        name: serviceName
      })).toBeVisible();
    }
  } finally {
    await context.close();
  }
});
