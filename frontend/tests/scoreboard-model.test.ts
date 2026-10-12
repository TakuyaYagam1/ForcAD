import assert from "node:assert/strict";
import test from "node:test";
import {
  getActiveTeamTasks,
  mapRawTeamTask,
  recalculateTeamScores,
} from "../src/entities/scoreboard/model/transforms.ts";
import type {
  RawTeamTask,
  ScoreboardTeam,
} from "../src/entities/scoreboard/model/store.ts";
import type { TeamTask } from "../src/entities/team-task/model/types.ts";
import { STATUS_OK } from "../src/shared/config/statuses.ts";

const teamOneId = "0199f7c0-0000-7000-8000-000000000001";
const teamTwoId = "0199f7c0-0000-7000-8000-000000000002";
const teamThreeId = "0199f7c0-0000-7000-8000-000000000003";
const taskId = "0199f7c0-0000-7000-8000-000000000010";

function team(id: string): ScoreboardTeam {
  return {
    id,
    name: `Team ${id}`,
    ip: `10.0.0.${id}`,
    highlighted: false,
    active: true,
    logo_path: "",
  };
}

function rawTeamTask(overrides: Partial<RawTeamTask> = {}): RawTeamTask {
  return {
    team_id: teamOneId,
    task_id: taskId,
    status: STATUS_OK,
    stolen: 0,
    lost: 0,
    score: 100,
    checks: 4,
    checks_passed: 4,
    message: null,
    ...overrides,
  };
}

function teamTask(teamId: string, score: number, sla: number): TeamTask {
  return {
    teamId,
    taskId,
    status: STATUS_OK,
    stolen: 0,
    lost: 0,
    checks: 1,
    checksPassed: 1,
    sla,
    score,
    message: "OK",
  };
}

test("scoreboard mapping clamps checks and filters inactive entities", () => {
  const mapped = mapRawTeamTask(
    rawTeamTask({ checks: 4, checks_passed: 7, message: null }),
  );
  assert.equal(mapped.teamId, teamOneId);
  assert.equal(mapped.taskId, taskId);
  assert.equal(mapped.checksPassed, 4);
  assert.equal(mapped.sla, 100);
  assert.equal(mapped.message, "OK");

  const active = getActiveTeamTasks(
    [
      rawTeamTask(),
      rawTeamTask({ team_id: "0199f7c0-0000-7000-8000-000000000009" }),
      rawTeamTask({ task_id: "0199f7c0-0000-7000-8000-000000000099" }),
    ],
    [team(teamOneId)],
    [{ id: taskId, name: "Service" }],
  );
  assert.deepEqual(active.map(({ teamId, taskId: activeTaskId }) => [teamId, activeTaskId]), [
    [teamOneId, taskId],
  ]);
});

test("team scores use SLA weighting and break ties by team ID", () => {
  const ranked = recalculateTeamScores(
    [team(teamThreeId), team(teamTwoId), team(teamOneId)],
    [
      teamTask(teamOneId, 200, 50),
      teamTask(teamTwoId, 100, 100),
      teamTask(teamThreeId, 150, 100),
    ],
  );

  assert.deepEqual(
    ranked.map(({ id, score }) => ({ id, score })),
    [
      { id: teamThreeId, score: 150 },
      { id: teamOneId, score: 100 },
      { id: teamTwoId, score: 100 },
    ],
  );
});
