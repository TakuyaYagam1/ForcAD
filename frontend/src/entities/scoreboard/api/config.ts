import { api } from "@/shared/lib/axios";

export interface ScoreboardConfig {
  round_time: number;
}

export async function fetchScoreboardConfig() {
  const { data } = await api.get<ScoreboardConfig>("/client/config/");
  return data;
}
