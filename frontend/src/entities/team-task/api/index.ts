import { api } from "@/shared/lib/axios";
import type { TeamTaskLogEntry } from "../model/types";

export async function fetchTeamTaskLog(teamId: number, taskId: number) {
  const { data } = await api.get<TeamTaskLogEntry[]>("/admin/teamtasks/", {
    params: {
      team_id: teamId,
      task_id: taskId,
    },
  });

  return data;
}
