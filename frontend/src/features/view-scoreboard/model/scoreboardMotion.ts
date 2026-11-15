import { roundDeltaForDisplay } from "./roundDelta.ts";

export interface ScoreMotionSnapshot {
  scope: string;
  resetPending: boolean;
  rows: {
    id: string;
    score: number | null
  }[];
}

export function snapshotScoreMotion(
  teams: readonly {
    id: string;
    score: number
  }[],
  taskIds: readonly string[],
  generation: number,
  resetPending: boolean,
): ScoreMotionSnapshot {
  return {
    resetPending,
    scope: JSON.stringify([generation, resetPending, taskIds]),
    rows: teams.map(({ id, score }) => {
      return {
        id,
        score: Number.isFinite(score) ? score : null,
      }
    }),
  };
}

/** Compare adjacent displayed packets; new data scopes establish a baseline. */
export function diffScoreMotion(
  previous: ScoreMotionSnapshot | null,
  current: ScoreMotionSnapshot,
) {
  const deltas = new Map<string, number>();
  const moved = new Set<string>();
  let leader: string | null = null;
  if (!previous?.rows.length || previous.scope !== current.scope || current.resetPending) {
    return {
      deltas,
      moved,
      leader
    };
  }
  const prior = new Map(previous.rows.map((row, rank) => {
    return [row.id, {
      ...row,
      rank
    }]
  }));
  current.rows.forEach((row, rank) => {
    const before = prior.get(row.id);
    if (!before) {
      return;
    }
    if (before.rank !== rank) {
      moved.add(row.id);
    }
    if (row.score !== null && before.score !== null) {
      const delta = roundDeltaForDisplay(row.score - before.score);
      if (delta !== 0 && Number.isFinite(delta)) {
        deltas.set(row.id, delta);
      }
    }
  });
  const first = current.rows[0]?.id;
  if (first && prior.has(first) && previous.rows[0]?.id !== first) {
    leader = first;
  }
  return {
    deltas,
    moved,
    leader
  };
}
