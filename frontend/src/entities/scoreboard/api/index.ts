import { api } from "@/shared/lib/axios";
import type { Team } from "@/entities/team/model/types";
import type { Task } from "@/entities/task/model/types";
import type { TeamTask } from "@/entities/team-task/model/types";

export interface ScoreboardInitResponse {
  state: {
    round: number;
    round_start: number | null;
    team_tasks: TeamTask[];
  };
  teams: Array<Omit<Team, "tasks" | "score" | "taskModels">>;
  tasks: Task[];
}

export async function fetchScoreboardInit() {
  const { data } = await api.get<ScoreboardInitResponse>("/scoreboard/init");
  return data;
}
