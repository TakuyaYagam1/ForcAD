import { getClientTeamHistory } from "@/shared/api/generated/endpoints";
import { infiniteQueryOptions, queryOptions, skipToken } from "@tanstack/react-query";
import type { TeamTaskStateRaw } from "../model/normalizeHistory";

export type { TeamTaskState, TeamTaskStateRaw } from "../model/normalizeHistory";

export const TEAM_HISTORY_PAGE_SIZE = 50;

export function teamHistoryQueryOptions(
  teamId: string | null,
  publishedRound: number,
  generation: number,
) {
  const queryKey = ["team-history", teamId, publishedRound, generation] as const;
  return queryOptions<TeamTaskStateRaw[]>({
    queryKey,
    queryFn: teamId === null
      ? skipToken
      : ({ signal }) => {
        return getClientTeamHistory(teamId, undefined, signal)
      },
    staleTime: 10_000,
  });
}

export function serviceHistoryQueryOptions(
  teamId: string | undefined,
  taskId: string | undefined,
  throughRound: number,
  generation: number,
) {
  return infiniteQueryOptions({
    queryKey: ["service-history", teamId, taskId, generation, throughRound],
    initialPageParam: throughRound,
    enabled: Boolean(teamId && taskId && throughRound > 0),
    queryFn: teamId && taskId
      ? ({ pageParam, signal }) => {
        return getClientTeamHistory(teamId, {
          task_id: taskId,
          through_round: pageParam,
        }, signal)
      }
      : skipToken,
    getNextPageParam: (page, _pages, through) => {
      const oldest = page.at(-1)?.round;
      if (page.length < TEAM_HISTORY_PAGE_SIZE || oldest === undefined || oldest <= 1 || oldest > through) {
        return undefined
      }
      return oldest - 1
    },
    // Completed snapshots are immutable within a game generation.
    staleTime: Infinity,
  });
}
