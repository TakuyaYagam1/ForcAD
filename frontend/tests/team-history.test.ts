import assert from "node:assert/strict";
import test from "node:test";
import {
  normalizeTeamTaskHistory,
  type TeamTaskStateRaw,
} from "../src/entities/team-scoreboard/model/normalizeHistory.ts";
import { STATUS_OK } from "../src/shared/config/statuses.ts";

const teamId = "0199f7c0-0000-7000-8000-000000000004";
const taskId = "0199f7c0-0000-7000-8000-000000000007";

function historyEntry(
  overrides: Partial<TeamTaskStateRaw> = {},
): TeamTaskStateRaw {
  return {
    team_id: teamId,
    task_id: taskId,
    round: 12,
    status: STATUS_OK,
    stolen: 1,
    lost: 0,
    score: 250,
    checks: 4,
    checks_passed: 3,
    timestamp: "1700000000-3",
    message: null,
    ...overrides,
  };
}

test("history is normalized, ordered newest first, and labels successful checks", () => {
  const history = normalizeTeamTaskHistory([
    historyEntry({ timestamp: "1700000000-1" }),
    historyEntry({ timestamp: "1700000001-0", status: 500, message: "failed" }),
    historyEntry({ timestamp: "1700000000-4", checks_passed: 8 }),
  ]);

  assert.deepEqual(
    history.map(({ timestampSecs, timestampNum }) => [timestampSecs, timestampNum]),
    [
      [1700000001, 0],
      [1700000000, 4],
      [1700000000, 1],
    ],
  );
  assert.equal(history[1]?.checksPassed, 4);
  assert.equal(history[1]?.sla, 100);
  assert.equal(history[1]?.message, "OK");
  assert.equal(history[1]?.id, `${teamId}:${taskId}:1700000000-4`);
  assert.equal(history[1]?.teamId, teamId);
  assert.equal(history[1]?.taskId, taskId);
  assert.equal(new Set(history.map(({ id }) => id)).size, 3);
});
