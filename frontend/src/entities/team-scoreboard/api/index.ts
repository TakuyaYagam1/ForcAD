import { getClientTeamHistory } from "@/shared/api/generated/endpoints";
import {
  normalizeTeamTaskHistory,
  type TeamTaskState,
  type TeamTaskStateRaw,
} from "../model/normalizeHistory";

export type { TeamTaskState, TeamTaskStateRaw };

export async function fetchTeamStates(
  teamId: string,
  signal?: AbortSignal,
): Promise<TeamTaskState[]> {
  const entries = await getClientTeamHistory(teamId, signal);
  return normalizeTeamTaskHistory(entries);
}
