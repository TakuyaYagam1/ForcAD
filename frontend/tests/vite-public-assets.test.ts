import assert from "node:assert/strict";
import {
  existsSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { build, createServer } from "vite";
import { createPublicAssetsPlugin } from "../vitePublicAssets.ts";

const teamLogo = '<svg xmlns="http://www.w3.org/2000/svg"></svg>';

test("public assets keep their URLs and team logos use the protected route", async () => {
  const projectDirectory = mkdtempSync(join(tmpdir(), "forcad-public-assets-"));
  const publicDirectory = join(projectDirectory, "public");
  const brandDirectory = join(publicDirectory, "brand");
  const teamDirectory = join(publicDirectory, "teams");
  const brandFile = join(brandDirectory, "mark.svg");
  const teamFile = join(teamDirectory, "fixture.svg");
  const plugin = createPublicAssetsPlugin(publicDirectory);

  try {
    mkdirSync(brandDirectory, { recursive: true });
    mkdirSync(teamDirectory, { recursive: true });
    writeFileSync(brandFile, teamLogo);
    writeFileSync(teamFile, teamLogo);
    writeFileSync(join(projectDirectory, "index.html"), "<html><body>SPA shell</body></html>");

    const server = await createServer({
      appType: "spa",
      configFile: false,
      logLevel: "silent",
      plugins: [plugin],
      publicDir: publicDirectory,
      root: projectDirectory,
      server: {
        host: "127.0.0.1",
        port: 0,
        strictPort: true,
      },
    });

    try {
      await server.listen();
      const address = server.httpServer?.address();
      assert.ok(address && typeof address !== "string");
      const origin = `http://127.0.0.1:${address.port}`;

      const teamsPage = await fetch(`${origin}/teams`);
      assert.equal(teamsPage.status, 200);
      assert.match(await teamsPage.text(), /SPA shell/);

      const brand = await fetch(`${origin}/brand/mark.svg`);
      assert.equal(brand.status, 200);
      assert.equal(await brand.text(), teamLogo);

      const logo = await fetch(`${origin}/team-logos/fixture.svg`);
      assert.equal(logo.status, 200);
      assert.equal(logo.headers.get("content-type"), "image/svg+xml");
      assert.equal(logo.headers.get("cache-control"), "no-cache");
      assert.equal(logo.headers.get("x-content-type-options"), "nosniff");
      assert.equal(
        logo.headers.get("content-security-policy"),
        "default-src 'none'; style-src 'unsafe-inline'; sandbox",
      );
      assert.equal(await logo.text(), teamLogo);

      const directLogo = await fetch(`${origin}/teams/fixture.svg`);
      assert.equal(directLogo.status, 404);

      const encodedDirectLogo = await fetch(`${origin}/%74eams/fixture.svg`);
      assert.equal(encodedDirectLogo.status, 404);
    } finally {
      await server.close();
    }

    const outputDirectory = join(projectDirectory, "dist");
    await build({
      configFile: false,
      logLevel: "silent",
      plugins: [createPublicAssetsPlugin(publicDirectory)],
      publicDir: publicDirectory,
      root: projectDirectory,
      build: {
        copyPublicDir: false,
        outDir: outputDirectory,
      },
    });

    assert.equal(readFileSync(join(outputDirectory, "brand/mark.svg"), "utf8"), teamLogo);
    assert.equal(
      readFileSync(join(outputDirectory, "team-logos/fixture.svg"), "utf8"),
      teamLogo,
    );
    assert.equal(existsSync(join(outputDirectory, "teams")), false);
  } finally {
    rmSync(projectDirectory, { recursive: true, force: true });
  }
});
