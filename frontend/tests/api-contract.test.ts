import assert from "node:assert/strict";
import test from "node:test";
import { AdminTeam } from "../src/shared/api/generated/schemas/admin/adminTeam.zod.ts";
import { AdminTeamTaskLogEntry } from "../src/shared/api/generated/schemas/admin/adminTeamTaskLogEntry.zod.ts";
import { GameConfig } from "../src/shared/api/generated/schemas/client/gameConfig.zod.ts";
import { ParticipantTask } from "../src/shared/api/generated/schemas/client/participantTask.zod.ts";
import { ParticipantTeamTaskHistory } from "../src/shared/api/generated/schemas/client/participantTeamTaskHistory.zod.ts";
import { RealtimeEvent } from "../src/shared/api/generated/schemas/realtimeEvent.zod.ts";
import { Scoreboard } from "../src/shared/api/generated/schemas/client/scoreboard.zod.ts";
import { parseRealtimeEvent } from "../src/shared/api/realtime.ts";

const teamId = "0199f7c0-0000-7000-8000-000000000003";
const taskId = "0199f7c0-0000-7000-8000-000000000008";
const historyId = "0199f7c0-0000-7000-8000-000000000108";

const runtime = {
  phase: "running",
  practice: false,
  reset_pending: false,
  generation: 4,
  scheduled_start: "2026-10-11T12:00:00Z",
  can_start: false,
  can_start_practice: false,
  round: 12,
  round_start: 1_791_719_200,
  paused_at: null,
  paused_seconds: 0,
  round_time: 120,
  round_waiting: false,
  results_pending: false,
};

const config = {
  id: 1,
  flag_lifetime: 3,
  game_hardness: 2,
  inflation: false,
  volga_attacks_mode: false,
  round_time: 120,
  rounds: null,
  mode: "demo",
  timezone: "UTC",
  start_time: "2026-10-11T12:00:00Z",
  real_round: 12,
  game_running: true,
};

const scoreboard = {
  state: {
    round_start: 1_791_719_200,
    round: 12,
    team_tasks: [
      {
        team_id: teamId,
        task_id: taskId,
        status: 101,
        stolen: 2,
        lost: 1,
        score: 900,
        checks: 10,
        checks_passed: 9,
        message: "",
      },
    ],
  },
  teams: [
    {
      id: teamId,
      name: "Northstar",
      ip: "10.10.14.8",
      highlighted: false,
      active: true,
      logo_path: "",
    },
  ],
  tasks: [{ id: taskId, name: "Vault" }],
  config,
  runtime,
};

test("generated scoreboard schema accepts the complete public snapshot", () => {
  assert.deepEqual(Scoreboard.parse(scoreboard), scoreboard);
  assert.equal(GameConfig.parse(config).id, 1);
});

test("generated entity schemas reject legacy numeric IDs and retain UUID log IDs", () => {
  const adminTeam = {
    id: teamId,
    name: "Northstar",
    ip: "10.10.14.8",
    token: "0123456789abcdef",
    highlighted: false,
    active: true,
    logo_path: "",
  };
  const historyEntry = {
    team_id: teamId,
    task_id: taskId,
    round: 12,
    status: 101,
    stolen: 2,
    lost: 1,
    score: 900,
    checks: 10,
    checks_passed: 9,
    timestamp: "1700000000-3",
    message: null,
  };
  const adminLogEntry = {
    id: historyId,
    round: 12,
    team_id: teamId,
    task_id: taskId,
    status: 101,
    score: 900,
    stolen: 2,
    lost: 1,
    checks: 10,
    checks_passed: 9,
    ts: 1_700_000_000_000,
    public_message: "OK",
    private_message: null,
    command: null,
  };

  assert.equal(AdminTeam.parse(adminTeam).id, teamId);
  assert.equal(ParticipantTask.parse({ id: taskId, name: "Vault" }).id, taskId);
  assert.equal("id" in ParticipantTeamTaskHistory.parse(historyEntry), false);
  assert.equal(AdminTeamTaskLogEntry.parse(adminLogEntry).id, historyId);
  assert.equal(AdminTeamTaskLogEntry.parse(adminLogEntry).ts, 1_700_000_000_000);

  assert.throws(() => AdminTeam.parse({ ...adminTeam, id: 3 }));
  assert.throws(() => AdminTeam.parse({ ...adminTeam, id: "not-a-uuid" }));
  assert.throws(() => ParticipantTask.parse({ id: 8, name: "Vault" }));
  assert.throws(() =>
    Scoreboard.parse({
      ...scoreboard,
      teams: [{ ...scoreboard.teams[0], id: 3 }],
    }),
  );
  assert.throws(() =>
    Scoreboard.parse({
      ...scoreboard,
      state: {
        ...scoreboard.state,
        team_tasks: [{ ...scoreboard.state.team_tasks[0], team_id: 3 }],
      },
    }),
  );
  assert.throws(() => ParticipantTeamTaskHistory.parse({ ...historyEntry, team_id: 3 }));
  assert.throws(() => ParticipantTeamTaskHistory.parse({ ...historyEntry, task_id: "8" }));
  assert.throws(() => AdminTeamTaskLogEntry.parse({ ...adminLogEntry, id: 108 }));
});

test("generated event schema accepts all public stream event envelopes", () => {
  const timestamp = "2026-10-11T12:00:00Z";
  const events = [
    {
      type: "init_scoreboard",
      payload: { data: scoreboard },
      timestamp,
    },
    {
      type: "update_scoreboard",
      payload: { data: scoreboard.state },
      timestamp,
    },
    {
      type: "flag_stolen",
      payload: {
        data: {
          attacker_id: teamId,
          victim_id: "0199f7c0-0000-7000-8000-000000000007",
          task_id: taskId,
          attacker_delta: 125.5,
          generation: 4,
        },
      },
      timestamp,
    },
  ];

  for (const event of events) {
    assert.deepEqual(RealtimeEvent.parse(event), event);
    assert.deepEqual(parseRealtimeEvent(JSON.stringify(event)), event);
  }
});

test("flag event parsing drops checker-private fields and rejects unknown events", () => {
  const parsed = parseRealtimeEvent(
    JSON.stringify({
      type: "flag_stolen",
      payload: {
        data: {
          attacker_id: teamId,
          victim_id: "0199f7c0-0000-7000-8000-000000000007",
          task_id: taskId,
          attacker_delta: 125.5,
          generation: 4,
          flag: "private-value",
          private_message: "checker output",
        },
      },
      timestamp: "2026-10-11T12:00:00Z",
    }),
  );

  assert.deepEqual(parsed, {
    type: "flag_stolen",
    payload: {
      data: {
        attacker_id: teamId,
        victim_id: "0199f7c0-0000-7000-8000-000000000007",
        task_id: taskId,
        attacker_delta: 125.5,
        generation: 4,
      },
    },
    timestamp: "2026-10-11T12:00:00Z",
  });
  assert.equal(parseRealtimeEvent("not json"), null);
  assert.equal(
    parseRealtimeEvent(
      JSON.stringify({
        type: "flag_stolen",
        payload: {
          data: {
            attacker_id: 3,
            victim_id: "0199f7c0-0000-7000-8000-000000000007",
            task_id: taskId,
            attacker_delta: 125,
            generation: 4,
          },
        },
        timestamp: "2026-10-11T12:00:00Z",
      }),
    ),
    null,
  );
  assert.equal(
    parseRealtimeEvent(
      JSON.stringify({ type: "admin_secret", payload: {}, timestamp: "now" }),
    ),
    null,
  );
});
