import type { TeamTaskState } from "../api";
export function groupHistoryByRound(
  states: TeamTaskState[],
  activeTaskIds: string[],
) {
  const active = new Set(activeTaskIds);
  const rounds = new Map<number, Map<string, TeamTaskState>>();
  // API order is newest first: keep the last checker result for each service and round.
  for (const state of states) {
    if (!active.has(state.taskId)) continue;
    const services =
      rounds.get(state.round) ?? new Map<string, TeamTaskState>();
    if (!services.has(state.taskId)) services.set(state.taskId, state);
    rounds.set(state.round, services);
  }
  return [...rounds]
    .sort(([a], [b]) => b - a)
    .map(([round, services]) => ({
      round,
      tasks: [...services.values()],
      incomplete: services.size < active.size,
      score: [...services.values()].reduce(
        (sum, value) => sum + (value.score * value.sla) / 100,
        0,
      ),
    }));
}
