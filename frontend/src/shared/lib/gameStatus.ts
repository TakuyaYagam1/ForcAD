import { getLocale, i18n } from "../i18n/index.ts";

export type GamePhase = "waiting" | "running" | "paused" | "finished" | "unknown";

const gameStatusKeys = {
  waiting: "gameStatus.waiting",
  running: "gameStatus.running",
  paused: "gameStatus.paused",
  finished: "gameStatus.finished",
  unknown: "gameStatus.unknown",
} as const satisfies Record<GamePhase, string>;

export function gameStatusLabel(phase: GamePhase): string {
  return String(
    i18n.t(gameStatusKeys[phase] ?? gameStatusKeys.unknown, {
      ns: "common",
      lng: getLocale(),
    }),
  );
}
