import assert from "node:assert/strict";
import { existsSync, statSync } from "node:fs";
import { resolve } from "node:path";
import test from "node:test";
import { createDemoTeams } from "../src/app/demo/data.ts";
import { demoTeams } from "../src/app/demo/teamLogos.ts";
import { resolveTeamLogoPath } from "../viteTeamLogos.ts";

test("demo uses the 20 server team names and shared logo paths", () => {
  const teams = createDemoTeams();

  assert.equal(teams.length, 20);
  assert.deepEqual(
    teams.map(({ name }) => name),
    [
      "danosito's ai farm",
      "daywave.",
      "Сборная г. Москва",
      "W0LV3S_CTF",
      "Trippy Troppy",
      "Caplag",
      "Молоток",
      "IBEEE",
      ".dot",
      "SEGFAULT",
      "мяу мяу",
      "Chetire",
      "Под Эгидой",
      "CUT",
      "c4ptur3_Th3_b0br",
      "SIGAN",
      "NON@me13",
      "HackTr1ckR0mP",
      "NCF (ex BEST IT)",
    ],
  );
  assert.deepEqual(
    teams.map(({ name, logo_path }) => [name, logo_path]),
    demoTeams.map(({ name, logo }) => [name, `/team-logos/${logo}`]),
  );
});

test("each demo team logo exists in the shared repository directory", () => {
  const logoDirectory = resolve(import.meta.dirname, "../../teams_logo");

  for (const { logo } of demoTeams) {
    const file = resolveTeamLogoPath(logoDirectory, logo);
    assert.ok(existsSync(file), `${logo} should exist`);
    assert.ok(statSync(file).size > 0, `${logo} should not be empty`);
  }
});
