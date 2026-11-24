import type { Team } from "@/entities/team/model/types";
import type { Task } from "@/entities/task/model/types";
import type { TeamTaskStateRaw } from "@/entities/team-scoreboard/api";
import type { RawTeamTask } from "@/entities/scoreboard/model/store";
import { STATUS_OK } from "../../shared/config/statuses.ts";

export const DEMO_ROUND_TIME = 120;
export const DEMO_INITIAL_ROUND = 128;
export const DEMO_DEFAULT_SCORE = 2500;
const DEMO_UUID_PREFIX = "0199f7c0-0000-7000-8000-";

export function demoUuid(sequence: number): string {
  return `${DEMO_UUID_PREFIX}${sequence.toString(16).padStart(12, "0")}`;
}

export interface DemoCaptureSeed {
  attackerId: string;
  victimId: string;
  taskId: string;
  delta: number;
  roundOffset: number;
}

export const DEMO_INITIAL_CAPTURE_EVENTS: readonly DemoCaptureSeed[] = [
  {
    attackerId: demoUuid(2),
    victimId: demoUuid(1),
    taskId: demoUuid(1001),
    delta: 185,
    roundOffset: 0,
  },
  {
    attackerId: demoUuid(3),
    victimId: demoUuid(5),
    taskId: demoUuid(1002),
    delta: 122.5,
    roundOffset: 0,
  },
  {
    attackerId: demoUuid(1),
    victimId: demoUuid(4),
    taskId: demoUuid(1003),
    delta: 210,
    roundOffset: 0,
  },
  {
    attackerId: demoUuid(5),
    victimId: demoUuid(7),
    taskId: demoUuid(1004),
    delta: 96.75,
    roundOffset: 0,
  },
  {
    attackerId: demoUuid(4),
    victimId: demoUuid(2),
    taskId: demoUuid(1001),
    delta: 154,
    roundOffset: 1,
  },
  {
    attackerId: demoUuid(6),
    victimId: demoUuid(8),
    taskId: demoUuid(1002),
    delta: 88.25,
    roundOffset: 1,
  },
  {
    attackerId: demoUuid(3),
    victimId: demoUuid(8),
    taskId: demoUuid(1002),
    delta: 61.5,
    roundOffset: 1,
  },
];

interface DemoCaptureTotals {
  stolen: number;
  lost: number;
  opponentFp: number;
  fpGained: number;
  scoreDelta: number;
}

type DemoCaptureTotalsByCell = Map<string, DemoCaptureTotals>;

function addCaptureTotals(
  totals: DemoCaptureTotalsByCell,
  event: DemoCaptureSeed,
  role: "attacker" | "victim",
) {
  const teamId = role === "attacker" ? event.attackerId : event.victimId;
  const key = `${teamId}:${event.taskId}`;
  const cell = totals.get(key) ?? {
    stolen: 0,
    lost: 0,
    opponentFp: 0,
    fpGained: 0,
    scoreDelta: 0,
  };
  if (role === "attacker") {
    cell.stolen += 1;
    cell.fpGained += event.delta;
    cell.scoreDelta += event.delta;
  } else {
    cell.lost += 1;
    cell.opponentFp += event.delta;
    cell.scoreDelta -= event.delta;
  }
  totals.set(key, cell);
}

function createDemoCaptureTotals(
  events: readonly DemoCaptureSeed[],
): DemoCaptureTotalsByCell {
  const totals: DemoCaptureTotalsByCell = new Map();
  for (const event of events) {
    addCaptureTotals(totals, event, "attacker");
    addCaptureTotals(totals, event, "victim");
  }
  return totals;
}

function createDemoCaptureTotalsByRound(
  events: readonly DemoCaptureSeed[],
  roundCount: number,
): Map<number, DemoCaptureTotalsByCell> {
  const totalsByRound = new Map<number, DemoCaptureTotalsByCell>();
  for (const event of events) {
    if (event.roundOffset < 0 || event.roundOffset >= roundCount) {
      continue;
    }
    let totals = totalsByRound.get(event.roundOffset);
    if (!totals) {
      totals = new Map();
      totalsByRound.set(event.roundOffset, totals);
    }
    addCaptureTotals(totals, event, "attacker");
    addCaptureTotals(totals, event, "victim");
  }
  return totalsByRound;
}

function mergeCaptureTotals(
  totals: DemoCaptureTotalsByCell,
  key: string,
  additions: DemoCaptureTotals,
) {
  const current = totals.get(key);
  if (current) {
    current.stolen += additions.stolen;
    current.lost += additions.lost;
    current.opponentFp += additions.opponentFp;
    current.fpGained += additions.fpGained;
    current.scoreDelta += additions.scoreDelta;
    return;
  }
  totals.set(key, {
    ...additions,
  });
}

const demoTeamNames = [
  "Northstar",
  "Byteforge",
  "Signal Works Security Research Group",
  "Blue Circuit",
  "Stackline",
  "Null Route",
  "Data Harbor",
  "Kernel Club",
  "Vector Team",
  "Packet Garden",
  "Night Shift",
  "Open Circuit",
  "Cache Unit",
  "Riverbed",
  "Safe Mode",
  "Bit Summit",
  "Wireframe",
  "Cloud Nine",
  "Loopback",
];

export function createDemoTeams(): Team[] {
  return demoTeamNames.map((name, index) => {
    return {
      id: demoUuid(index + 1),
      name,
      ip: `10.10.${14 + index}.8`,
      token: `demo-team-${index + 1}`,
      kind: "player",
      logo_path: "",
      highlighted: index === 3,
      active: true,
    }
  });
}

export function createDemoTasks(): Task[] {
  return ["Vault", "Market", "Notes", "Cloud", "Mail"].map((name, index) => {
    return {
      id: demoUuid(1001 + index),
      name,
      checker: `demo/${name.toLowerCase()}/checker.py`,
      gets: 1,
      puts: 1,
      places: 2,
      checker_timeout: 20,
      checker_type: "hackerdom",
      env_path: "",
      default_score: DEMO_DEFAULT_SCORE,
      get_period: 10,
      active: true,
    }
  });
}

export function createDemoTeamTasks(
  teams: Team[],
  tasks: Task[],
): RawTeamTask[] {
  const examples = [STATUS_OK, STATUS_OK, STATUS_OK, STATUS_OK, 102, 104, 103, 110];
  const captureTotals = createDemoCaptureTotals(DEMO_INITIAL_CAPTURE_EVENTS);
  return teams.flatMap((team, index) => {
    return tasks.map((task, column) => {
      const status =
        column === 1 ? examples[index % examples.length] : STATUS_OK;
      const teamId = team.id!;
      const taskId = task.id!;
      const captures = captureTotals.get(`${teamId}:${taskId}`);
      return {
        team_id: teamId,
        task_id: taskId,
        score: Math.max(
          0,
          4800 - index * 350 - column * 90 + (captures?.scoreDelta ?? 0),
        ),
        stolen: Math.max(0, 16 - index + column + (captures?.stolen ?? 0)),
        lost: index + column + (captures?.lost ?? 0),
        checks: 100,
        checks_passed: 100 - index - column,
        status,
        message: null,
      }
    })
  });
}

export function createDemoHistory(
  raw: RawTeamTask[],
  round: number,
  nowSecs: number,
): TeamTaskStateRaw[] {
  const taskOrder = [...new Set(raw.map((item) => {
    return item.task_id
  }))];
  const captureTotalsByRound = createDemoCaptureTotalsByRound(
    DEMO_INITIAL_CAPTURE_EVENTS,
    round,
  );
  const newerCaptureTotals: DemoCaptureTotalsByCell = new Map();

  return Array.from({
    length: round,
  }, (_, offset) => {
    const roundCaptureTotals = captureTotalsByRound.get(offset);
    const snapshots = raw.map((item) => {
      const key = `${item.team_id}:${item.task_id}`;
      const roundCaptures = roundCaptureTotals?.get(key);
      const newerCaptures = newerCaptureTotals.get(key);
      return {
        ...item,
        round: round - offset,
        status: offset % 4 >= 2 ? STATUS_OK : item.status,
        score: Math.max(
          0,
          item.score - offset * 45 - (newerCaptures?.scoreDelta ?? 0),
        ),
        stolen: Math.max(0, item.stolen - (newerCaptures?.stolen ?? 0)),
        lost: Math.max(0, item.lost - (newerCaptures?.lost ?? 0)),
        round_flags_captured: roundCaptures?.stolen ?? 0,
        round_fp_gained: roundCaptures?.fpGained ?? 0,
        round_flags_lost: roundCaptures?.lost ?? 0,
        round_opponent_fp: roundCaptures?.opponentFp ?? 0,
        timestamp: `${nowSecs - offset * DEMO_ROUND_TIME}-${taskOrder.indexOf(item.task_id) + 1}`,
      }
    });
    if (roundCaptureTotals) {
      for (const [key, captures] of roundCaptureTotals) {
        mergeCaptureTotals(newerCaptureTotals, key, captures);
      }
    }
    return snapshots;
  }).flat();
}
