interface RankedTeam {
  id: string;
  score: number;
}

interface ServiceResult {
  teamId: string;
  taskId: string;
  status: number;
  score: number;
  checks: number;
  checksPassed: number;
  sla: number;
  stolen: number;
  lost: number;
  message: string;
}

export interface ScoreboardSnapshot {
  columns: string;
  rows: [string, string][];
}

/** Compare displayed results, independently of transport object identity/order. */
export function snapshotScoreboard(
  teams: readonly RankedTeam[],
  tasks: readonly {
    id: string
  }[],
  results: readonly ServiceResult[],
): ScoreboardSnapshot {
  const cells = new Map(results.map((result) => {
    return [
      `${result.teamId}:${result.taskId}`,
      result,
    ]
  }));

  return {
    columns: JSON.stringify(tasks.map(({ id }) => {
      return id
    })),
    rows: teams.map(({ id, score }, rank) => {
      return [id, JSON.stringify([
        rank,
        score,
        tasks.map((task) => {
          const value = cells.get(`${id}:${task.id}`);
          return value ? [
            value.status, value.score, value.checks, value.checksPassed,
            value.sla, value.stolen, value.lost, value.message,
          ] : null;
        }),
      ])]
    }),
  };
}

export function changedScoreboardRows(
  previous: ScoreboardSnapshot | null,
  current: ScoreboardSnapshot,
): string[] {
  // Initial data and changed service columns establish a new baseline.
  if (!previous?.rows.length || previous.columns !== current.columns) {
    return [];
  }
  const previousRows = new Map(previous.rows);
  return current.rows
    .filter(([id, signature]) => {
      return previousRows.get(id) !== signature
    })
    .map(([id]) => {
      return id
    });
}
