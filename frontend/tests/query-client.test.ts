import assert from "node:assert/strict";
import test from "node:test";
import { QueryClient, QueryObserver } from "@tanstack/react-query";
import {
  refreshGameQueries,
  setQuerySnapshot,
} from "../src/shared/lib/queryClient.ts";

const queryKeys = [
  "game-runtime",
  "team-history",
  "teamtask-log",
  "team",
  "task",
  "admin-teams",
  "admin-tasks",
] as const;

async function waitForIdle(client: QueryClient) {
  let idleTicks = 0;
  for (let attempt = 0; attempt < 100; attempt += 1) {
    await new Promise((resolve) => setTimeout(resolve, 0));
    if (client.isFetching() === 0) idleTicks += 1;
    else idleTicks = 0;
    if (idleTicks === 2) return;
  }
  throw new Error("Queries did not settle.");
}

test("init snapshots skip runtime refetch and keep other query groups fresh", async () => {
  const client = new QueryClient();
  const requests = new Map(queryKeys.map((key) => [key, 0]));
  const observers = queryKeys.map((key) => new QueryObserver(client, {
    queryKey: [key],
    queryFn: async () => {
      requests.set(key, requests.get(key)! + 1);
      return null;
    },
    retry: false,
    staleTime: Infinity,
  }));
  const unsubscribe = observers.map((observer) => observer.subscribe(() => {}));

  try {
    await waitForIdle(client);
    for (const key of queryKeys) requests.set(key, 0);

    setQuerySnapshot(client, ["game-runtime"], { phase: "running", generation: 4 });
    refreshGameQueries(client);
    await waitForIdle(client);

    assert.deepEqual(Object.fromEntries(requests), {
      "game-runtime": 0,
      "team-history": 1,
      "teamtask-log": 1,
      team: 1,
      task: 1,
      "admin-teams": 1,
      "admin-tasks": 1,
    });
  } finally {
    unsubscribe.forEach((stop) => stop());
    client.clear();
  }
});

test("late REST responses cannot replace newer runtime and scoreboard snapshots", async () => {
  const client = new QueryClient();
  const olderRuntime = { phase: "waiting", generation: 3 };
  const newerRuntime = { phase: "running", generation: 4 };
  const olderScoreboard = { state: { round: 3 }, runtime: olderRuntime };
  const newerScoreboard = { state: { round: 4 }, runtime: newerRuntime };
  let resolveStatus!: (value: typeof olderRuntime) => void;
  let resolveScoreboard!: (value: typeof olderScoreboard) => void;
  const statusRequest = client.fetchQuery({
    queryKey: ["game-runtime"],
    queryFn: () => new Promise<typeof olderRuntime>((resolve) => {
      resolveStatus = resolve;
    }),
    retry: false,
  });
  const scoreboardRequest = client.fetchQuery({
    queryKey: ["scoreboardInit"],
    queryFn: () => new Promise<typeof olderScoreboard>((resolve) => {
      resolveScoreboard = resolve;
    }),
    retry: false,
  });

  try {
    for (let attempt = 0; attempt < 100 && client.isFetching() < 2; attempt += 1) {
      await new Promise((resolve) => setTimeout(resolve, 0));
    }
    assert.equal(client.isFetching(), 2);

    setQuerySnapshot(client, ["game-runtime"], newerRuntime);
    setQuerySnapshot(client, ["scoreboardInit"], newerScoreboard);
    resolveStatus(olderRuntime);
    resolveScoreboard(olderScoreboard);
    await Promise.all([
      statusRequest.catch(() => undefined),
      scoreboardRequest.catch(() => undefined),
    ]);
    await waitForIdle(client);

    assert.deepEqual(client.getQueryData(["game-runtime"]), newerRuntime);
    assert.deepEqual(client.getQueryData(["scoreboardInit"]), newerScoreboard);
  } finally {
    client.clear();
  }
});
