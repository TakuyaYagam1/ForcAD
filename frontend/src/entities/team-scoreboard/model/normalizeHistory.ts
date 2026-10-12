import type { ParticipantTeamTaskHistory } from "../../../shared/api/generated/schemas/client/participantTeamTaskHistory.zod.ts";
import { STATUS_OK } from "../../../shared/config/statuses.ts";
import { finiteNumber } from "../../../shared/lib/numbers.ts";
import { getSlaPercent } from "../../../shared/lib/sla.ts";

export type TeamTaskStateRaw = ParticipantTeamTaskHistory;

export interface TeamTaskState {
  id: string;
  round: number;
  taskId: string;
  teamId: string;
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

function mapHistoryEntry(entry: TeamTaskStateRaw): TeamTaskState {
  const timestampSeparator = entry.timestamp.indexOf("-");
  const timestampSecs = Number(entry.timestamp.slice(0, timestampSeparator));
  const timestampNum = Number(entry.timestamp.slice(timestampSeparator + 1));
  const checks = Math.max(0, Math.trunc(finiteNumber(entry.checks)));
  const checksPassed = Math.min(
    checks,
    Math.max(0, Math.trunc(finiteNumber(entry.checks_passed))),
  );
  const status = finiteNumber(entry.status);
  const message = entry.message ?? "";

  return {
    id: `${entry.team_id}:${entry.task_id}:${entry.timestamp}`,
    round: finiteNumber(entry.round),
    taskId: entry.task_id,
    teamId: entry.team_id,
    status,
    stolen: finiteNumber(entry.stolen),
    lost: finiteNumber(entry.lost),
    score: finiteNumber(entry.score),
    checks,
    checksPassed,
    sla: getSlaPercent(checks, checksPassed) ?? 0,
    message: message === "" && status === STATUS_OK ? "OK" : message,
    timestampSecs,
    timestampNum,
  };
}

export function normalizeTeamTaskHistory(
  entries: TeamTaskStateRaw[],
): TeamTaskState[] {
  return entries.map(mapHistoryEntry).sort((first, second) => {
    if (first.timestampSecs === second.timestampSecs) {
      return second.timestampNum - first.timestampNum;
    }
    return second.timestampSecs - first.timestampSecs;
  });
}
