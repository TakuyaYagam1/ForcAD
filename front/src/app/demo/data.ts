import type { Team } from "@/entities/team/model/types";
import type { Task } from "@/entities/task/model/types";
import type { TeamTaskStateRaw } from "@/entities/team-scoreboard/api";
import type { RawTeamTask } from "@/entities/scoreboard/model/store";

export const DEMO_ROUND_TIME = 120;
export const DEMO_INITIAL_ROUND = 128;

const teamExamples = [
  ["Red Team", "red-team.jpg"],
  ["Orion", "orion.jpg"],
  ["Alt+F4", "alt-f4.png"],
  ["RedRaven", "red-raven.jpg"],
  ["IRKSEC", "irksec.png"],
  ["V7 Peace", "v7-peace.jpg"],
  ["Tuxedo", "tuxedo.png"],
  ["Команда без логотипа", ""],
];

export function createDemoTeams(): Team[] {
  return teamExamples.map(([name, logo], index) => ({
    id: index + 1,
    name,
    ip: `10.10.${14 + index}.8`,
    token: `demo-team-${index + 1}`,
    logo_path: logo
      ? new URL(`/demo/logos/${logo}`, window.location.origin).href
      : "",
    highlighted: index === 3,
    active: true,
  }));
}

export function createDemoTasks(): Task[] {
  return ["Vault", "Market", "Notes", "Cloud"].map((name, index) => ({
    id: index + 1,
    name,
    checker: `demo/${name.toLowerCase()}/checker.py`,
    gets: 1,
    puts: 1,
    places: 2,
    checker_timeout: 20,
    checker_type: "hackerdom",
    env_path: "",
    default_score: 2500,
    get_period: 10,
    active: true,
  }));
}

export function createDemoTeamTasks(
  teams: Team[],
  tasks: Task[],
): RawTeamTask[] {
  const examples = [101, 101, 101, 101, 102, 104, 103, 110];
  return teams.flatMap((team, index) =>
    tasks.map((task, column) => {
      const status = column === 1 ? examples[index] : 101;
      return {
        team_id: team.id!,
        task_id: task.id!,
        score: 4800 - index * 350 - column * 90,
        stolen: 16 - index + column,
        lost: index + column,
        checks: 100,
        checks_passed: 100 - index - column,
        status,
        message:
          status === 101
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
  return Array.from({ length: 7 }, (_, offset) =>
    raw.map((item, index) => ({
      ...item,
      id: offset * raw.length + index + 1,
      round: round - offset,
      score: Math.max(0, item.score - offset * 45),
      stolen: Math.max(0, item.stolen - offset),
      lost: Math.max(0, item.lost - Math.floor(offset / 2)),
      timestamp: `${nowSecs - offset * DEMO_ROUND_TIME}-${item.task_id}`,
    })),
  ).flat();
}
