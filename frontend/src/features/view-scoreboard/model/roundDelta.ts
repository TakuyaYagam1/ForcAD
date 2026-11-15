import type { TeamTaskState } from "../../../entities/team-scoreboard/model/normalizeHistory.ts";

export interface RoundDeltaScope {
  generation: number;
  round: number;
  teamId: string;
  taskIds: string[];
}

export interface RoundDeltaHistory {
  scopeKey: string;
  states: TeamTaskState[];
}

export type RoundDelta =
  | {
    status: "ready";
    value: number
  }
  | {
    status: "missing" | "incomplete" | "stale"
  };

/** Scoreboard snapshots contain completed scores; runtime describes the active round. */
export function getCompletedScoreRound(snapshotRound: number, runtimeRound: number | null) {
  if (!Number.isInteger(snapshotRound) || snapshotRound < 1) {
    return null;
  }
  const round = runtimeRound === null
    ? snapshotRound
    : Math.min(snapshotRound, runtimeRound - 1);
  return Number.isInteger(round) && round > 0 ? round : null;
}

export function roundDeltaScopeKey(scope: RoundDeltaScope) {
  return JSON.stringify([
    scope.generation,
    scope.round,
    scope.teamId,
    [...new Set(scope.taskIds)].sort(),
  ]);
}

/** Align the sign and color with the two-decimal score shown to the reader. */
export function roundDeltaForDisplay(value: number) {
  const rounded = Math.sign(value) * Math.round((Math.abs(value) + Number.EPSILON) * 100) / 100;
  return Object.is(rounded, -0) ? 0 : rounded;
}

/** Both endpoints of the difference must exist and contain every active service. */
export function calculateRoundDelta(history: RoundDeltaHistory, scope: RoundDeltaScope): RoundDelta {
  if (history.scopeKey !== roundDeltaScopeKey(scope)) {
    return {
      status: "stale"
    };
  }
  const taskIds = new Set(scope.taskIds);
  if (!taskIds.size || scope.round < 1) {
    return {
      status: "missing"
    };
  }
  const rounds = new Map<number, Map<string, TeamTaskState>>();
  for (const state of history.states) {
    if (state.teamId !== scope.teamId || !taskIds.has(state.taskId) ||
      (state.round !== scope.round && state.round !== scope.round - 1)) {
      continue;
    }
    const tasks = rounds.get(state.round) ?? new Map<string, TeamTaskState>();
    const previous = tasks.get(state.taskId);
    // Select the latest checker result regardless of transport order or duplicates.
    if (!previous || state.timestampSecs > previous.timestampSecs ||
      (state.timestampSecs === previous.timestampSecs && state.timestampNum > previous.timestampNum)) {
      tasks.set(state.taskId, state);
    }
    rounds.set(state.round, tasks);
  }
  const current = rounds.get(scope.round);
  const previous = rounds.get(scope.round - 1);
  if (!current || !previous) {
    return {
      status: "missing"
    };
  }
  if (current.size !== taskIds.size || previous.size !== taskIds.size) {
    return {
      status: "incomplete"
    };
  }
  const score = (tasks: Map<string, TeamTaskState>) => {
    return [...tasks.values()].reduce((sum, task) => {
      return sum + task.score * task.sla / 100;
    }, 0);
  };
  const value = score(current) - score(previous);
  if (!Number.isFinite(value)) {
    return {
      status: "incomplete"
    };
  }
  return {
    status: "ready",
    value: Object.is(value, -0) ? 0 : value
  };
}
