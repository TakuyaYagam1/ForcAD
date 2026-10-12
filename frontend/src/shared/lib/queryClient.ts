import { QueryClient, type QueryKey } from "@tanstack/react-query";
export const queryClient = new QueryClient();

export function setQuerySnapshot<TData>(
  client: QueryClient,
  queryKey: QueryKey,
  data: TData,
) {
  void client.cancelQueries({ queryKey, exact: true });
  client.setQueryData(queryKey, data);
}

export function refreshGameQueries(client: QueryClient = queryClient) {
  for (const key of [
    "team-history",
    "teamtask-log",
    "team",
    "task",
    "admin-teams",
    "admin-tasks",
  ]) {
    void client.invalidateQueries({ queryKey: [key] });
  }
}
