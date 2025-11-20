import { api } from "@/shared/lib/axios";

export interface TeamTaskStateRaw {
  id: number;
  round: number;
  task_id: number;
  team_id: number;
  status: number;
  stolen: number;
  lost: number;
  score: number;
  checks: number;
  checks_passed: number;
  timestamp: string;
  message: string;
}

export interface TeamTaskState {
  id: number;
  round: number;
  taskId: number;
  teamId: number;
  status: number;
  stolen: number;
  lost: number;
  score: number;
  sla: number;
  message: string;
  timestampSecs: number;
  timestampNum: number;
}

export async function fetchTeamStates(
  teamId: number
): Promise<TeamTaskState[]> {
  const { data } = await api.get<TeamTaskStateRaw[]>(
    `/client/teams/${teamId}/`
  );

  const mapped: TeamTaskState[] = data.map((x) => {
    const tsSecs = Number(x.timestamp.slice(0, x.timestamp.indexOf("-")));
    const tsNum = Number(x.timestamp.slice(x.timestamp.indexOf("-") + 1));
    const sla = (100.0 * x.checks_passed) / Math.max(x.checks, 1);

    const msg = x.message === "" && x.status === 101 ? "OK" : x.message;

    return {
      id: Number(x.id),
      round: Number(x.round),
      taskId: Number(x.task_id),
      teamId: Number(x.team_id),
      status: x.status,
      stolen: x.stolen,
      lost: x.lost,
      score: Number(x.score),
      sla,
      message: msg,
      timestampSecs: tsSecs,
      timestampNum: tsNum,
    };
  });

  // как во Vue: сортировка по timestamp (новые сначала)
  mapped.sort((a, b) => {
    if (a.timestampSecs === b.timestampSecs) {
      return b.timestampNum - a.timestampNum;
    }
    return b.timestampSecs - a.timestampSecs;
  });

  return mapped;
}
