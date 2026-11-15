import assert from "node:assert/strict";
import test from "node:test";
import {
  calculateRoundDelta,
  getCompletedScoreRound,
  roundDeltaScopeKey,
  roundDeltaForDisplay,
  type RoundDeltaScope,
} from "../src/features/view-scoreboard/model/roundDelta.ts";
import { normalizeTeamTaskHistory } from "../src/entities/team-scoreboard/model/normalizeHistory.ts";
import { createDemoHistory, createDemoTeamTasks, createDemoTasks, createDemoTeams } from "../src/app/demo/data.ts";

const scope: RoundDeltaScope = {
  generation: 7,
  round: 12,
  teamId: "team-one",
  taskIds: ["task-a", "task-b"],
};

function state(round: number, taskId: string, score: number, overrides = {}) {
  return normalizeTeamTaskHistory([{
    team_id: scope.teamId,
    task_id: taskId,
    round,
    score,
    checks: 4,
    checks_passed: 4,
    stolen: 0,
    lost: 0,
    status: 101,
    message: "",
    timestamp: `${1700000000 + round}-1`,
    ...overrides,
  }])[0];
}

const baseline = [state(11, "task-a", 100), state(11, "task-b", 200)];
function delta(states: ReturnType<typeof state>[], target = scope) {
  return calculateRoundDelta({
    scopeKey: roundDeltaScopeKey(target),
    states,
  }, target);
}

test("display precision makes near-zero differences neutral without changing the historical calculation", () => {
  const result = delta([...baseline, state(12, "task-a", 100.0000000001), state(12, "task-b", 200)]);
  assert.equal(result.status, "ready");
  if (result.status === "ready") {
    assert.ok(result.value > 0);
    assert.equal(roundDeltaForDisplay(result.value), 0);
  }
  assert.equal(roundDeltaForDisplay(-0.000000001), 0);
  assert.equal(Object.is(roundDeltaForDisplay(-0.000000001), -0), false);
  assert.equal(roundDeltaForDisplay(1.234), 1.23);
  assert.equal(roundDeltaForDisplay(-1.234), -1.23);
  assert.equal(roundDeltaForDisplay(0.005), 0.01);
  assert.equal(roundDeltaForDisplay(-0.005), -0.01);
});

test("delta compares complete consecutive rounds using SLA-weighted totals", () => {
  assert.deepEqual(delta([...baseline, state(12, "task-a", 160), state(12, "task-b", 200)]), {
    status: "ready",
    value: 60,
  });
  assert.deepEqual(delta([...baseline, state(12, "task-a", 160), state(12, "task-b", 200, {
    checks_passed: 2
  })]), {
    status: "ready",
    value: -40,
  });
  assert.deepEqual(delta([...baseline, state(12, "task-a", 100), state(12, "task-b", 200)]), {
    status: "ready",
    value: 0,
  });
});

test("snapshot and runtime identify the completed round, never the active one", () => {
  assert.equal(getCompletedScoreRound(12, 13), 12);
  assert.equal(getCompletedScoreRound(13, 13), 12);
  assert.equal(getCompletedScoreRound(11, 13), 11);
  assert.equal(getCompletedScoreRound(12, null), 12);
  assert.equal(getCompletedScoreRound(0, 1), null);
  assert.equal(getCompletedScoreRound(12, 0), null);
  assert.equal(getCompletedScoreRound(NaN, 13), null);
});

test("missing rounds and incomplete service sets remain unavailable, never zero", () => {
  assert.deepEqual(delta([...baseline, state(12, "task-a", 400)]), {
    status: "incomplete"
  });
  assert.deepEqual(delta([state(10, "task-a", 100), state(10, "task-b", 200), state(12, "task-a", 100), state(12, "task-b", 200)]), {
    status: "missing"
  });
  assert.deepEqual(delta([]), {
    status: "missing"
  });
});

test("latest checker result wins and repeated same-round transport data leaves the delta stable", () => {
  const current = [state(12, "task-a", 170), state(12, "task-b", 200)];
  const result = delta([...baseline, ...current]);
  const repeated = delta([...current, ...baseline, ...current, state(12, "task-a", 900, {
    timestamp: "1600000000-1"
  }), ...baseline]);
  assert.deepEqual(result, {
    status: "ready",
    value: 70
  });
  assert.deepEqual(repeated, result);
  assert.deepEqual(delta([...baseline, ...current, state(12, "task-a", 180, {
    timestamp: "1700000012-2"
  })]), {
    status: "ready",
    value: 80
  });
});

test("other teams, inactive services and the active round cannot alter the completed delta", () => {
  const current = [state(12, "task-a", 100), state(12, "task-b", 200)];
  assert.deepEqual(delta([...baseline, ...current, state(12, "task-a", 800, {
    team_id: "another-team",
    timestamp: "1800000000-1"
  }), state(12, "removed-task", 900), state(13, "task-a", 999)]), {
    status: "ready",
    value: 0,
  });
});

test("cache scopes reject late responses from old generations, schemas and rounds", () => {
  const history = {
    scopeKey: roundDeltaScopeKey(scope),
    states: [...baseline, state(12, "task-a", 100), state(12, "task-b", 200)],
  };
  for (const changed of [{
    ...scope,
    generation: 8
  }, {
    ...scope,
    round: 13
  }, {
    ...scope,
    taskIds: ["task-a", "new-task"]
  }, {
    ...scope,
    teamId: "another-team"
  }]) {
    assert.deepEqual(calculateRoundDelta(history, changed), {
      status: "stale"
    });
  }
  assert.equal(roundDeltaScopeKey(scope), roundDeltaScopeKey({
    ...scope,
    taskIds: [...scope.taskIds].reverse(),
  }));
});

test("demo history matches its current snapshot and produces actual gains, losses and ties", () => {
  const teams = createDemoTeams();
  const tasks = createDemoTasks();
  const raw = createDemoTeamTasks(teams, tasks);
  const states = normalizeTeamTaskHistory(createDemoHistory(raw, 128, 1800000000));
  const changes = teams.map((team) => {
    const target = {
      ...scope,
      round: 128,
      teamId: team.id!,
      taskIds: tasks.map((task) => {
        return task.id!;
      }),
    };
    const result = delta(states, target);
    assert.equal(result.status, "ready");
    return result.status === "ready" ? result.value : NaN;
  });
  assert.ok(changes.some((value) => {
    return value > 0;
  }));
  assert.ok(changes.some((value) => {
    return value < 0;
  }));
  assert.ok(changes.some((value) => {
    return value === 0;
  }));
  for (const item of raw) {
    assert.equal(states.find((value) => {
      return value.round === 128 && value.teamId === item.team_id && value.taskId === item.task_id;
    })?.score, item.score);
  }
});
