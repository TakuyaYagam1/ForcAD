import { Buffer } from "node:buffer";
import { expect, test } from "@playwright/test";
import { createE2EContext, loginAsAdmin, readAdminCredentials } from "./support";

test("game settings reject a damaged backup before confirmation", async ({ browser }) => {
  const context = await createE2EContext(browser);
  try {
    const page = await context.newPage();
    await loginAsAdmin(page, await readAdminCredentials());
    await page.getByTestId("game-settings-trigger").click();
    const panel = page.getByRole("region", {
      name: "Резервная копия"
    });
    await expect(panel.getByRole("button", {
      name: "Экспорт .tar.gz"
    })).toBeDisabled();
    const importButton = panel.getByRole("button", {
      name: "Импорт .tar.gz"
    });
    await expect(importButton).toBeEnabled();
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
