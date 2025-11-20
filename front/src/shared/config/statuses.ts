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
    label: "UP",
    description: "Service is up",
    color: "#166534", // bg-emerald-900
    badgeClassName: "bg-emerald-500/20 text-emerald-300 border-emerald-500/40",
  },
  {
    code: 102,
    label: "CORRUPT",
    description: "Corrupt response",
    color: "#854d0e", // bg-amber-900
    badgeClassName: "bg-amber-500/15 text-amber-300 border-amber-500/40",
  },
  {
    code: 103,
    label: "MUMBLE",
    description: "Mumble / partial failure",
    color: "#7c2d12", // bg-orange-900
    badgeClassName: "bg-orange-500/15 text-orange-300 border-orange-500/40",
  },
  {
    code: 104,
    label: "DOWN",
    description: "Service is down",
    color: "#7f1d1d", // bg-red-900
    badgeClassName: "bg-red-500/15 text-red-300 border-red-500/40",
  },
  {
    code: 110,
    label: "CHECKER_ERROR",
    description: "Checker error",
    color: "#312e81", // bg-indigo-900
    badgeClassName: "bg-indigo-500/15 text-indigo-300 border-indigo-500/40",
  },
];

export const STATUS_COLOR_BY_CODE: Record<number, string> = Object.fromEntries(
  SCOREBOARD_STATUSES.map((s) => [s.code, s.color])
);

export const STATUS_META_BY_CODE: Record<number, ScoreboardStatusMeta> =
  Object.fromEntries(SCOREBOARD_STATUSES.map((s) => [s.code, s]));
