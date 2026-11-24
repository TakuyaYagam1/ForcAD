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
import {
  createDemoHistory,
  createDemoTasks,
  createDemoTeamTasks,
  createDemoTeams,
  demoUuid,
  DEMO_INITIAL_CAPTURE_EVENTS,
  DEMO_INITIAL_ROUND,
} from "../src/app/demo/data.ts";
import { STATUS_OK } from "../src/shared/config/statuses.ts";
import { resolveTeamLogoPath } from "../vitePublicAssets.ts";

test("demo roster is synthetic and lets the generic avatar fallback render", () => {
  const teams = createDemoTeams();

  assert.equal(teams.length, 19);
  assert.equal(teams[0].name, "Northstar");
  assert.equal(teams[3].name, "Blue Circuit");
  assert.equal(teams.every(({ logo_path }) => {
    return !logo_path
  }), true);
  assert.equal(new Set(teams.map(({ name }) => {
    return name
  })).size, teams.length);
});

test("demo tasks append Mail without shifting existing seed IDs or statuses", () => {
  const tasks = createDemoTasks();
  assert.deepEqual(
    tasks.map(({ name }) => {
      return name
    }),
    ["Vault", "Market", "Notes", "Cloud", "Mail"],
  );
  assert.deepEqual(
    tasks.slice(0, 4).map(({ id }) => {
      return id
    }),
    [1001, 1002, 1003, 1004].map((sequence) => {
      return demoUuid(sequence)
    }),
  );
  assert.equal(tasks[4]?.id, demoUuid(1005));

  const teams = createDemoTeams();
  const raw = createDemoTeamTasks(teams, tasks);
  const fifthTeamStatuses = raw
    .filter(({ team_id }) => {
      return team_id === teams[4]!.id
    })
    .map(({ status }) => {
      return status
    });
  assert.deepEqual(
    fifthTeamStatuses,
    [STATUS_OK, 102, STATUS_OK, STATUS_OK, STATUS_OK],
  );
});

test("demo history includes every completed round with its seed captures", () => {
  const teams = createDemoTeams();
  const tasks = createDemoTasks();
  const raw = createDemoTeamTasks(teams, tasks);
  const history = createDemoHistory(raw, DEMO_INITIAL_ROUND, 1_000);
  const rounds = [
    ...new Set(
      history.map(({ round }) => {
        return round
      }),
    ),
  ];
  assert.deepEqual(
    rounds,
    Array.from(
      {
        length: DEMO_INITIAL_ROUND,
      },
      (_, offset) => {
        return DEMO_INITIAL_ROUND - offset
      },
    ),
  );
  assert.equal(history.length, raw.length * DEMO_INITIAL_ROUND);

  type CaptureTotals = {
    flagsCaptured: number;
    fpGained: number;
    flagsLost: number;
    opponentFp: number;
  };
  const totalsByCell = new Map<string, CaptureTotals>();
  const totalsFor = (round: number, teamId: string, taskId: string) => {
    const key = `${round}:${teamId}:${taskId}`;
    let totals = totalsByCell.get(key);
    if (!totals) {
      totals = {
        flagsCaptured: 0,
        fpGained: 0,
        flagsLost: 0,
        opponentFp: 0,
      };
      totalsByCell.set(key, totals);
    }
    return totals;
  };

  for (const event of DEMO_INITIAL_CAPTURE_EVENTS) {
    const eventRound = DEMO_INITIAL_ROUND - event.roundOffset;
    const captured = totalsFor(eventRound, event.attackerId, event.taskId);
    captured.flagsCaptured += 1;
    captured.fpGained += event.delta;

    const lost = totalsFor(eventRound, event.victimId, event.taskId);
    lost.flagsLost += 1;
    lost.opponentFp += event.delta;
  }

  for (const snapshot of history) {
    const expected = totalsByCell.get(
      `${snapshot.round}:${snapshot.team_id}:${snapshot.task_id}`,
    );
    assert.equal(snapshot.round_flags_captured, expected?.flagsCaptured ?? 0);
    assert.equal(snapshot.round_fp_gained, expected?.fpGained ?? 0);
    assert.equal(snapshot.round_flags_lost, expected?.flagsLost ?? 0);
    assert.equal(snapshot.round_opponent_fp, expected?.opponentFp ?? 0);
  }
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
  context.after(() => {
    return rmSync(root, {
      recursive: true,
      force: true
    })
  });

  assert.equal(resolveTeamLogoPath(logoDirectory, "team-one.png"), insideFile);
  assert.equal(resolveTeamLogoPath(logoDirectory, "team-two.svg"), insideSvg);
  assert.equal(resolveTeamLogoPath(logoDirectory, "../outside.png"), null);
  assert.equal(resolveTeamLogoPath(logoDirectory, "linked.png"), null);
  assert.equal(resolveTeamLogoPath(logoDirectory, "team-one.pdf"), null);
});
