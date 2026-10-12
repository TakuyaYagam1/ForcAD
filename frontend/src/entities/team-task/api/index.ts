import { getAdminTeamTaskHistory } from "@/shared/api/generated/endpoints";

export async function fetchTeamTaskLog(
  teamId: string,
  taskId: string,
  signal?: AbortSignal,
) {
  return getAdminTeamTaskHistory({
    team_id: teamId,
    task_id: taskId,
  }, signal);
}
