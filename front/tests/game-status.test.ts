import assert from "node:assert/strict";
import test from "node:test";
import { gameStatusLabel } from "../src/shared/lib/gameStatus.ts";

test("game lifecycle has distinct public status labels", () => {
  assert.equal(gameStatusLabel("running"), "Игра идет");
  assert.equal(gameStatusLabel("paused"), "Игра приостановлена");
  assert.equal(gameStatusLabel("finished"), "Игра завершилась");
  assert.equal(gameStatusLabel("waiting"), "Ожидание старта");
  assert.equal(gameStatusLabel("unknown"), "Статус игры недоступен");
});
