import { useEffect, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { getGameEventsSocket } from "@/shared/lib/socket";
import {
  useScoreboardStore,
  type GameRuntimeStatus,
  type InitScoreboardPayload,
  type GameStatePayload,
} from "@/entities/scoreboard/model/store";
import { api } from "@/shared/lib/axios";
import { refreshGameQueries } from "@/shared/lib/queryClient";
export function GameEventsProvider({ children }: { children: ReactNode }) {
  const runtime = useQuery({
    queryKey: ["game-runtime"],
    queryFn: async () =>
      (await api.get<GameRuntimeStatus>("/client/status/")).data,
    refetchInterval: 5000,
    retry: 2,
  });
  useEffect(() => {
    if (runtime.isError) {
      useScoreboardStore.getState().invalidateRuntime();
    } else if (runtime.data) {
      useScoreboardStore.getState().setRuntime(runtime.data);
    }
  }, [runtime.data, runtime.dataUpdatedAt, runtime.isError]);
  useEffect(() => {
    const socket = getGameEventsSocket();
    const onConnect = () => {
      useScoreboardStore.getState().setConnected(true);
      refreshGameQueries();
    };
    const onDisconnect = () => {
      useScoreboardStore.getState().setConnected(false);
      useScoreboardStore.getState().setError("Соединение с сервером потеряно");
    };
    const onInit = ({ data }: { data: InitScoreboardPayload }) => {
      useScoreboardStore.getState().handleInitScoreboardMessage(data);
      refreshGameQueries();
    };
    const onUpdate = ({ data }: { data: GameStatePayload }) => {
      useScoreboardStore.getState().handleUpdateScoreboardMessage(data);
      refreshGameQueries();
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
  }, []);
  return <>{children}</>;
}
