import { QueryClient } from "@tanstack/react-query";
export const queryClient = new QueryClient();
export function refreshGameQueries() {
  for (const key of [
    "game-runtime",
    "team-history",
    "teamtask-log",
    "team",
    "task",
    "admin-teams",
    "admin-tasks",
  ]) {
    void queryClient.invalidateQueries({ queryKey: [key] });
  }
}
