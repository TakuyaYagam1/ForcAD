// src/entities/scoreboard/api/useScoreboardInit.ts
import { useQuery } from "@tanstack/react-query";
import { fetchScoreboardInit } from "./index";

export function useScoreboardInit() {
  return useQuery({
    queryKey: ["scoreboardInit"],
    queryFn: fetchScoreboardInit,
    refetchInterval: 5_000, // Refresh every 5 seconds.
  });
}
