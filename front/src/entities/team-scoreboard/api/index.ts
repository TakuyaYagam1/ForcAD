import { finiteNumber } from "@/shared/lib/numbers";
import { getSlaPercent } from "@/shared/lib/sla";
import { api } from "@/shared/lib/axios";

export interface TeamTaskStateRaw {
  id?: number;
  round: number;
  task_id?: number;
  team_id?: number;
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
  id: string;
  round: number;
  taskId: number;
  teamId: number;
  status: number;
  stolen: number;
  lost: number;
  score: number;
  checks: number;
  checksPassed: number;
  sla: number;
  message: string;
  timestampSecs: number;
  timestampNum: number;
}

export async function fetchTeamStates(
  teamId: number,
): Promise<TeamTaskState[]> {
  const { data } = await api.get<TeamTaskStateRaw[]>(
    `/client/teams/${teamId}/`,
  );

  const mapped: TeamTaskState[] = data.map((x) => {
    const tsSecs = Number(x.timestamp.slice(0, x.timestamp.indexOf("-")));
    const tsNum = Number(x.timestamp.slice(x.timestamp.indexOf("-") + 1));
    const checks = Math.max(0, Math.trunc(finiteNumber(x.checks)));
    const checksPassed = Math.min(
      checks,
      Math.max(0, Math.trunc(finiteNumber(x.checks_passed))),
    );
    const sla = getSlaPercent(checks, checksPassed) ?? 0;

    const msg =
      x.message === "" && finiteNumber(x.status) === 101 ? "OK" : x.message;

    return {
      id: `${x.team_id}:${x.task_id}:${x.timestamp}`,
      round: finiteNumber(x.round),
      taskId: finiteNumber(x.task_id),
      teamId: finiteNumber(x.team_id),
      status: finiteNumber(x.status),
      stolen: finiteNumber(x.stolen),
      lost: finiteNumber(x.lost),
      score: finiteNumber(x.score),
      checks,
      checksPassed,
      sla,
      message: msg,
      timestampSecs: tsSecs,
      timestampNum: tsNum,
    };
  });

  // Sort by timestamp, newest first.
  mapped.sort((a, b) => {
    if (a.timestampSecs === b.timestampSecs) {
      return b.timestampNum - a.timestampNum;
    }
    return b.timestampSecs - a.timestampSecs;
  });

  return mapped;
}
