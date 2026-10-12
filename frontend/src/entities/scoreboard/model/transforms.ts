import type { TeamTask } from "../../team-task/model/types.ts";
import type {
  InitScoreboardPayload,
  RawTeamTask,
  ScoreboardTeam,
} from "./store.ts";
import { STATUS_OK } from "../../../shared/config/statuses.ts";
import { finiteNumber } from "../../../shared/lib/numbers.ts";
import { getSlaPercent } from "../../../shared/lib/sla.ts";
import { compareUuid } from "../../../shared/lib/uuid.ts";

export function mapRawTeamTask(rawTeamTask: RawTeamTask): TeamTask {
  const checks = Math.max(0, Math.trunc(finiteNumber(rawTeamTask.checks)));
  const checksPassed = Math.min(
    checks,
    Math.max(0, Math.trunc(finiteNumber(rawTeamTask.checks_passed))),
  );
  const status = finiteNumber(rawTeamTask.status, -1);

  return {
    teamId: rawTeamTask.team_id,
    taskId: rawTeamTask.task_id,
    status,
    stolen: finiteNumber(rawTeamTask.stolen),
    lost: finiteNumber(rawTeamTask.lost),
    checks,
    checksPassed,
    sla: getSlaPercent(checks, checksPassed) ?? 0,
    score: finiteNumber(rawTeamTask.score),
    message: rawTeamTask.message
      ? String(rawTeamTask.message)
      : status === STATUS_OK
        ? "OK"
        : "",
  };
}

export function getActiveTeamTasks(
  rawTeamTasks: RawTeamTask[],
  teams: ScoreboardTeam[],
  tasks: InitScoreboardPayload["tasks"],
): TeamTask[] {
  const teamIds = new Set(teams.map((team) => team.id));
  const taskIds = new Set(tasks.map((task) => task.id));

  return rawTeamTasks
    .map(mapRawTeamTask)
    .filter(
      (teamTask) =>
        teamIds.has(teamTask.teamId) && taskIds.has(teamTask.taskId),
    );
}

export function recalculateTeamScores(
  teams: ScoreboardTeam[],
  teamTasks: TeamTask[],
): ScoreboardTeam[] {
  const scoresByTeamId = new Map<string, number>();

  for (const teamTask of teamTasks) {
    const weightedScore = (teamTask.score * teamTask.sla) / 100;
    scoresByTeamId.set(
      teamTask.teamId,
      (scoresByTeamId.get(teamTask.teamId) ?? 0) + weightedScore,
    );
  }

  return teams
    .map((team) => ({
      ...team,
      score: scoresByTeamId.get(team.id) ?? 0,
    }))
    .sort(
      (first, second) =>
        (second.score ?? 0) - (first.score ?? 0) ||
        compareUuid(first.id, second.id),
    );
}
