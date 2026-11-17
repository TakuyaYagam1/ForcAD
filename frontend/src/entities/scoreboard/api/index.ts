import { getScoreboard } from "@/shared/api/generated/endpoints";
import type { Scoreboard } from "@/shared/api/generated/schemas/client/scoreboard.zod";

export type ScoreboardInitResponse = Scoreboard;

export async function fetchScoreboardInit(signal?: AbortSignal) {
  return getScoreboard(signal);
}
