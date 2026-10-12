import type { Team } from "@/entities/team/model/types";
import type { Task } from "@/entities/task/model/types";
import type { TeamTaskStateRaw } from "@/entities/team-scoreboard/api";
import type { RawTeamTask } from "@/entities/scoreboard/model/store";
import { STATUS_OK } from "../../shared/config/statuses.ts";

export const DEMO_ROUND_TIME = 120;
export const DEMO_INITIAL_ROUND = 128;
export const DEMO_DEFAULT_SCORE = 2500;
const DEMO_UUID_PREFIX = "0199f7c0-0000-7000-8000-";

export function demoUuid(sequence: number): string {
  return `${DEMO_UUID_PREFIX}${sequence.toString(16).padStart(12, "0")}`;
}

const demoTeamNames = [
  "Northstar",
  "Byteforge",
  "Signal Works",
  "Blue Circuit",
  "Stackline",
  "Null Route",
  "Data Harbor",
  "Kernel Club",
  "Vector Team",
  "Packet Garden",
  "Night Shift",
  "Open Circuit",
  "Cache Unit",
  "Riverbed",
  "Safe Mode",
  "Bit Summit",
  "Wireframe",
  "Cloud Nine",
  "Loopback",
];

export function createDemoTeams(): Team[] {
  return demoTeamNames.map((name, index) => ({
    id: demoUuid(index + 1),
    name,
    ip: `10.10.${14 + index}.8`,
    token: `demo-team-${index + 1}`,
    logo_path: "",
    highlighted: index === 3,
    active: true,
  }));
}

export function createDemoTasks(): Task[] {
  return ["Vault", "Market", "Notes", "Cloud"].map((name, index) => ({
    id: demoUuid(1001 + index),
    name,
    checker: `demo/${name.toLowerCase()}/checker.py`,
    gets: 1,
    puts: 1,
    places: 2,
    checker_timeout: 20,
    checker_type: "hackerdom",
    env_path: "",
    default_score: DEMO_DEFAULT_SCORE,
    get_period: 10,
    active: true,
  }));
}

export function createDemoTeamTasks(
  teams: Team[],
  tasks: Task[],
): RawTeamTask[] {
  const examples = [STATUS_OK, STATUS_OK, STATUS_OK, STATUS_OK, 102, 104, 103, 110];
  return teams.flatMap((team, index) =>
    tasks.map((task, column) => {
      const status =
        column === 1 ? examples[index % examples.length] : STATUS_OK;
      return {
        team_id: team.id!,
        task_id: task.id!,
        score: Math.max(0, 4800 - index * 350 - column * 90),
        stolen: Math.max(0, 16 - index + column),
        lost: index + column,
        checks: 100,
        checks_passed: 100 - index - column,
        status,
        message:
          status === STATUS_OK
            ? ""
            : status === 104
              ? "Сервис недоступен: соединение отклонено"
              : status === 103
                ? "Нарушена целостность сохранённых данных"
                : status === 102
                  ? "Проверка ответа сервиса завершилась с ошибкой"
                  : "Сервис временно отключён",
      };
    }),
  );
}

export function createDemoHistory(
  raw: RawTeamTask[],
  round: number,
  nowSecs: number,
): TeamTaskStateRaw[] {
  const taskOrder = [...new Set(raw.map((item) => item.task_id))];

  return Array.from({ length: 7 }, (_, offset) =>
    raw.map((item) => ({
      ...item,
      round: round - offset,
      score: Math.max(0, item.score - offset * 45),
      stolen: Math.max(0, item.stolen - offset),
      lost: Math.max(0, item.lost - Math.floor(offset / 2)),
      timestamp: `${nowSecs - offset * DEMO_ROUND_TIME}-${taskOrder.indexOf(item.task_id) + 1}`,
    })),
  ).flat();
}
