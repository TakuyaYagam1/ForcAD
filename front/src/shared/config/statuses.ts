// src/shared/config/statuses.ts

export type ScoreboardStatusCode = 101 | 102 | 103 | 104 | 110;

export interface ScoreboardStatusMeta {
  code: ScoreboardStatusCode;
  label: string;
  description?: string;
  color: string; // hex для фона ячейки
  badgeClassName?: string; // tailwind-классы для бейджа
}

export const SCOREBOARD_STATUSES: ScoreboardStatusMeta[] = [
  {
    code: 101,
    label: "В норме",
    description: "Сервис работает штатно (UP)",
    color: "#6be273",
    badgeClassName: "bg-emerald-500/20 text-emerald-300 border-emerald-500/40",
  },
  {
    code: 102,
    label: "Повреждён",
    description: "Повреждённый ответ сервиса (CORRUPT)",
    color: "#ffd078",
    badgeClassName: "bg-amber-500/15 text-amber-300 border-amber-500/40",
  },
  {
    code: 103,
    label: "Ошибка ответа",
    description: "Ошибка или неполный ответ сервиса (MUMBLE)",
    color: "#ffa35e",
    badgeClassName: "bg-orange-500/15 text-orange-300 border-orange-500/40",
  },
  {
    code: 104,
    label: "Недоступен",
    description: "Сервис недоступен (DOWN)",
    color: "#ff4554",
    badgeClassName: "bg-red-500/15 text-red-300 border-red-500/40",
  },
  {
    code: 110,
    label: "Ошибка чекера",
    description: "Ошибка проверки сервиса (CHECKER_ERROR)",
    color: "#78b9e6",
    badgeClassName: "bg-slate-500/15 text-slate-300 border-slate-500/40",
  },
];

export const STATUS_COLOR_BY_CODE: Record<number, string> = Object.fromEntries(
  SCOREBOARD_STATUSES.map((s) => [s.code, s.color]),
);

export const STATUS_META_BY_CODE: Record<number, ScoreboardStatusMeta> =
  Object.fromEntries(SCOREBOARD_STATUSES.map((s) => [s.code, s]));
