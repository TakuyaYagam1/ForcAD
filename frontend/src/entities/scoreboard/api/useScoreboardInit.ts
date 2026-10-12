// src/entities/scoreboard/api/useScoreboardInit.ts
import { useQuery } from "@tanstack/react-query";
import { fetchScoreboardInit } from "./index";

export function useScoreboardInit() {
  return useQuery({
    queryKey: ["scoreboardInit"],
    queryFn: ({ signal }) => fetchScoreboardInit(signal),
    staleTime: Infinity,
  });
}
