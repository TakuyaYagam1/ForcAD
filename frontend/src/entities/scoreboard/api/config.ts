import { getClientConfig } from "@/shared/api/generated/endpoints";
import type { GameConfig } from "@/shared/api/generated/schemas/client/gameConfig.zod";

export type ScoreboardConfig = Pick<GameConfig, "round_time">;

export async function fetchScoreboardConfig(signal?: AbortSignal) {
  return getClientConfig(signal);
}
