import assert from "node:assert/strict";
import test from "node:test";
import { roundTimerSeconds } from "../src/features/view-scoreboard/model/roundTimer.ts";

test("round timer starts over at the round boundary", () => {
  assert.equal(roundTimerSeconds(59, 60), 59);
  assert.equal(roundTimerSeconds(60, 60), 0);
  assert.equal(roundTimerSeconds(61, 60), 1);
  assert.equal(roundTimerSeconds(120, 60), 0);
});
