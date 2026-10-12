import { useEffect, useRef, type ReactNode } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { getGameEventsSocket } from "@/shared/lib/socket";
import {
  useScoreboardStore,
  type InitScoreboardPayload,
  type GameStatePayload,
} from "@/entities/scoreboard/model/store";
import { useScoreboardInit } from "@/entities/scoreboard/api/useScoreboardInit";
import type { Scoreboard } from "@/shared/api/generated/schemas/client/scoreboard.zod";
import { getClientStatus } from "@/shared/api/generated/endpoints";
import { refreshGameQueries, setQuerySnapshot } from "@/shared/lib/queryClient";

function initialSnapshotPayload(scoreboard: Scoreboard): InitScoreboardPayload {
  return {
    state: scoreboard.state,
    teams: scoreboard.teams,
    tasks: scoreboard.tasks,
    config: { round_time: scoreboard.config.round_time },
    runtime: scoreboard.runtime,
  };
}

export function GameEventsProvider({ children }: { children: ReactNode }) {
  const queryClient = useQueryClient();
  const scoreboard = useScoreboardInit();
  const appliedSnapshot = useRef<object | null>(null);
  const latestUpdate = useRef<GameStatePayload | null>(null);
  const runtime = useQuery({
    queryKey: ["game-runtime"],
    queryFn: ({ signal }) => getClientStatus(signal),
    refetchInterval: 5000,
    retry: 2,
  });
  useEffect(() => {
    if (runtime.isError && !runtime.data) {
      useScoreboardStore.getState().invalidateRuntime();
    } else if (runtime.data) {
      useScoreboardStore.getState().setRuntime(runtime.data);
    }
  }, [runtime.data, runtime.dataUpdatedAt, runtime.isError]);

  useEffect(() => {
    const snapshot = scoreboard.data;
    if (!snapshot || snapshot === appliedSnapshot.current) return;
    if (queryClient.getQueryData(["scoreboardInit"]) !== snapshot) return;

    appliedSnapshot.current = snapshot;
    if (snapshot.runtime) {
      setQuerySnapshot(queryClient, ["game-runtime"], snapshot.runtime);
    }
    useScoreboardStore
      .getState()
      .handleInitScoreboardMessage(initialSnapshotPayload(snapshot));
    if (latestUpdate.current) {
      useScoreboardStore
        .getState()
        .handleUpdateScoreboardMessage(latestUpdate.current);
    }
  }, [queryClient, scoreboard.data]);

  useEffect(() => {
    const socket = getGameEventsSocket();
    const onConnect = () => {
      latestUpdate.current = null;
      useScoreboardStore.getState().setConnected(true);
      void queryClient.refetchQueries({ queryKey: ["scoreboardInit"], exact: true });
      void queryClient.refetchQueries({ queryKey: ["game-runtime"], exact: true });
      refreshGameQueries(queryClient);
    };
    const onDisconnect = () => {
      useScoreboardStore.getState().setConnected(false);
      useScoreboardStore.getState().setError("Соединение с сервером потеряно");
    };
    const onInit = ({ data }: { data: InitScoreboardPayload }) => {
      latestUpdate.current = null;
      appliedSnapshot.current = data;
      setQuerySnapshot(queryClient, ["scoreboardInit"], data);
      if (data.runtime) {
        setQuerySnapshot(queryClient, ["game-runtime"], data.runtime);
      }
      useScoreboardStore.getState().handleInitScoreboardMessage(data);
      refreshGameQueries(queryClient);
    };
    const onUpdate = ({ data }: { data: GameStatePayload }) => {
      latestUpdate.current = data;
      useScoreboardStore.getState().handleUpdateScoreboardMessage(data);
      refreshGameQueries(queryClient);
    };
    socket.on("connect", onConnect);
    socket.on("disconnect", onDisconnect);
    socket.on("connect_error", onDisconnect);
    socket.on("init_scoreboard", onInit);
    socket.on("update_scoreboard", onUpdate);
    if (socket.connected) onConnect();
    return () => {
      socket.off("connect", onConnect);
      socket.off("disconnect", onDisconnect);
      socket.off("connect_error", onDisconnect);
      socket.off("init_scoreboard", onInit);
      socket.off("update_scoreboard", onUpdate);
    };
  }, [queryClient]);
  return <>{children}</>;
}
