import assert from "node:assert/strict";
import test from "node:test";
import { i18n, getLanguage, getLocale, resources, setLanguage } from "../src/shared/i18n/index.ts";
import { resolveLanguage } from "../src/shared/i18n/locale.ts";
import { formatScore } from "../src/shared/lib/formatScore.ts";
import { gameStatusLabel } from "../src/shared/lib/gameStatus.ts";
import { SCOREBOARD_STATUSES, STATUS_META_BY_CODE } from "../src/shared/config/statuses.ts";
import { formatTeamValidationIssue } from "../src/shared/lib/adminValidation.ts";

function entries(value: object, path = ""): Array<[string, string]> {
  return Object.entries(value).flatMap(([key, child]) => {
    const name = path ? `${path}.${key}` : key;
    return typeof child === "string" ? [[name, child]] : entries(child, name);
  });
}

test("RU and EN cover the same messages and interpolation variables", () => {
  for (const namespace of ["common", "public", "admin", "shell"] as const) {
    const ru = new Map(entries(resources.ru[namespace]));
    const en = new Map(entries(resources.en[namespace]));
    assert.deepEqual([...ru.keys()].sort(), [...en.keys()].sort(), namespace);
    for (const [key, text] of ru) {
      const variables = (value: string) => [...value.matchAll(/\{\{([^}]+)\}\}/g)]
        .map((match) => match[1]).sort();
      assert.deepEqual(variables(text), variables(en.get(key)!), `${namespace}:${key}`);
      assert.ok(en.get(key)?.trim(), `${namespace}:${key} is empty`);
    }
  }
});

test("only RU and EN are supported, with Russian as the default", async () => {
  assert.equal(resolveLanguage(null), "ru");
  assert.equal(resolveLanguage("de"), "ru");
  assert.equal(resolveLanguage("en"), "en");
  assert.equal(getLanguage(), "ru");
  const status = STATUS_META_BY_CODE[101];
  try {
    await setLanguage("en");
    assert.equal(getLanguage(), "en");
    assert.equal(getLocale(), "en-US");
    assert.equal(formatScore(1234.5), "1,234.5");
    assert.equal(gameStatusLabel("paused"), "Game paused");
    assert.deepEqual(SCOREBOARD_STATUSES.map(({ label }) => label), ["UP", "CORRUPT", "MUMBLE", "DOWN", "CHECK FAILED"]);
    assert.equal(status.description, "Service is operating normally (UP)");
    assert.equal(formatTeamValidationIssue({ kind: "ip" }), "Enter a valid IPv4 or IPv6 address.");
    assert.equal(i18n.t("public:scoreboard.teamCount", { count: 2, formattedCount: "2" }), "2 teams");
    await setLanguage("ru");
    assert.equal(getLocale(), "ru-RU");
    assert.match(formatScore(1234.5), /^1\s234,5$/);
    assert.deepEqual(SCOREBOARD_STATUSES.map(({ label }) => label), ["UP", "CORRUPT", "MUMBLE", "DOWN", "CHECK FAILED"]);
    assert.equal(status.description, "Сервис работает штатно (UP)");
    for (const [count, word] of [[1, "команда"], [2, "команды"], [5, "команд"], [21, "команда"]] as const) {
      assert.equal(i18n.t("public:scoreboard.teamCount", { count, formattedCount: String(count) }), `${count} ${word}`);
    }
  } finally {
    await setLanguage("ru");
  }
});
