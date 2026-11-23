import { Buffer } from "node:buffer";
import { readFile } from "node:fs/promises";
import { expect, test } from "@playwright/test";
import { createE2EContext, loginAsAdmin, readAdminCredentials } from "./support";

test("a clean board exports an importable setup and rejects a damaged archive", async ({ browser }) => {
  const context = await createE2EContext(browser);
  try {
    const page = await context.newPage();
    await loginAsAdmin(page, await readAdminCredentials());
    await page.getByTestId("game-settings-trigger").click();
    const panel = page.getByRole("region", {
      name: "Резервная копия"
    });
    const exportButton = panel.getByRole("button", {
      name: "Экспорт .tar.gz"
    });
    const [download] = await Promise.all([page.waitForEvent("download"), exportButton.click()]);
    const importButton = panel.getByRole("button", {
      name: "Импорт .tar.gz"
    });
    await expect(importButton).toBeEnabled();
    const file = await download.path();
    if (!file) {
      throw new Error("The exported archive was not downloaded");
    }
    const [validChooser] = await Promise.all([page.waitForEvent("filechooser"), importButton.click()]);
    await validChooser.setFiles({
      name: download.suggestedFilename(),
      mimeType: "application/gzip",
      buffer: await readFile(file),
    });
    await download.delete();
    await expect(panel.getByText(/Раунд 0,/)).toBeVisible();
    await expect(panel.getByRole("button", {
      name: "Восстановить игру"
    })).toBeEnabled();
    await panel.getByRole("button", {
      name: "Отмена"
    }).click();
    const [chooser] = await Promise.all([page.waitForEvent("filechooser"), importButton.click()]);
    await chooser.setFiles({
      name: "damaged.tar.gz",
      mimeType: "application/gzip",
      buffer: Buffer.from("not a gzip archive"),
    });
    await expect(panel.getByRole("alert")).toContainText("Архив повреждён");
    await expect(panel.getByRole("button", {
      name: "Восстановить игру"
    })).toHaveCount(0);
  } finally {
    await context.close();
  }
});
