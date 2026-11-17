// src/shared/config/statuses.ts

import { i18n } from "../i18n/index.ts";

export const STATUS_OK = 101 as const;
export type ScoreboardStatusCode = typeof STATUS_OK | 102 | 103 | 104 | 110;

export interface ScoreboardStatusMeta {
  code: ScoreboardStatusCode;
  label: string;
  description?: string;
  color: string; // Saturated status background
  foreground: string; // Readable foreground on the status background
  badgeClassName?: string; // Tailwind classes for the badge
}

type StatusDefinition = Omit<ScoreboardStatusMeta, "description">;

const statusDescriptionKeys = {
  [STATUS_OK]: "status.ok.description",
  102: "status.corrupt.description",
  103: "status.mumble.description",
  104: "status.down.description",
  110: "status.checkerError.description",
} as const satisfies Record<ScoreboardStatusCode, string>;

const statusDefinitions: StatusDefinition[] = [
  {
    code: STATUS_OK,
    label: "UP",
    color: "#22b34b",
    foreground: "#141414",
    badgeClassName: "bg-emerald-500/20 text-emerald-300 border-emerald-500/40",
  },
  {
    code: 102,
    label: "CORRUPT",
    color: "#dfb719",
    foreground: "#141414",
    badgeClassName: "bg-amber-500/15 text-amber-300 border-amber-500/40",
  },
  {
    code: 103,
    label: "MUMBLE",
    color: "#e4771c",
    foreground: "#141414",
    badgeClassName: "bg-orange-500/15 text-orange-300 border-orange-500/40",
  },
  {
    code: 104,
    label: "DOWN",
    color: "#d63248",
    foreground: "#ffffff",
    badgeClassName: "bg-red-500/15 text-red-300 border-red-500/40",
  },
  {
    code: 110,
    label: "CHECK FAILED",
    color: "#1079bd",
    foreground: "#ffffff",
    badgeClassName: "bg-slate-500/15 text-slate-300 border-slate-500/40",
  },
];

function createStatusMeta(definition: StatusDefinition): ScoreboardStatusMeta {
  const descriptionKey = statusDescriptionKeys[definition.code];

  return {
    ...definition,
    get description() {
      return String(i18n.t(descriptionKey, {
        ns: "common"
      }));
    },
  };
}

export const SCOREBOARD_STATUSES: ScoreboardStatusMeta[] =
  statusDefinitions.map(createStatusMeta);

export const STATUS_COLOR_BY_CODE: Record<number, string> = Object.fromEntries(
  SCOREBOARD_STATUSES.map((s) => {
    return [s.code, s.color]
  }),
);

export const STATUS_META_BY_CODE: Record<number, ScoreboardStatusMeta> =
  Object.fromEntries(SCOREBOARD_STATUSES.map((s) => {
    return [s.code, s]
  }));
