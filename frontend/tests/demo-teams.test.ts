import assert from "node:assert/strict";
import {
  mkdirSync,
  mkdtempSync,
  rmSync,
  symlinkSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { createDemoTeams } from "../src/app/demo/data.ts";
import { resolveTeamLogoPath } from "../vitePublicAssets.ts";

test("demo roster is synthetic and lets the generic avatar fallback render", () => {
  const teams = createDemoTeams();

  assert.equal(teams.length, 19);
  assert.equal(teams[0].name, "Northstar");
  assert.equal(teams[3].name, "Blue Circuit");
  assert.equal(teams.every(({ logo_path }) => !logo_path), true);
  assert.equal(new Set(teams.map(({ name }) => name)).size, teams.length);
});

test("team logo path resolver accepts files in its directory only", (context) => {
  const root = mkdtempSync(join(tmpdir(), "forcad-logo-test-"));
  const logoDirectory = join(root, "logos");
  const outsideFile = join(root, "outside.png");
  const insideFile = join(logoDirectory, "team-one.png");
  const insideSvg = join(logoDirectory, "team-two.svg");
  const escapeLink = join(logoDirectory, "linked.png");
  mkdirSync(logoDirectory);
  writeFileSync(insideFile, "logo");
  writeFileSync(insideSvg, "<svg xmlns=\"http://www.w3.org/2000/svg\"/>");
  writeFileSync(outsideFile, "outside");
  symlinkSync(outsideFile, escapeLink);
  context.after(() => rmSync(root, { recursive: true, force: true }));

  assert.equal(resolveTeamLogoPath(logoDirectory, "team-one.png"), insideFile);
  assert.equal(resolveTeamLogoPath(logoDirectory, "team-two.svg"), insideSvg);
  assert.equal(resolveTeamLogoPath(logoDirectory, "../outside.png"), null);
  assert.equal(resolveTeamLogoPath(logoDirectory, "linked.png"), null);
  assert.equal(resolveTeamLogoPath(logoDirectory, "team-one.pdf"), null);
});
