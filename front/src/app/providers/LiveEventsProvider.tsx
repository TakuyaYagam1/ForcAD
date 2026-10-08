import { useEffect, type ReactNode } from "react";
import { getLiveEventsSocket } from "@/shared/lib/socket";
import {
  useLiveScoreboardStore,
  type FlagNotificationPayload,
} from "@/entities/live-scoreboard/model/store";
export function LiveEventsProvider({ children }: { children: ReactNode }) {
  useEffect(() => {
    const socket = getLiveEventsSocket();
    const onReady = () => useLiveScoreboardStore.getState().setError(null);
    const onError = () =>
      useLiveScoreboardStore
        .getState()
        .setError("Соединение с лентой событий потеряно");
    const onEvent = (
      payload: FlagNotificationPayload | { data: FlagNotificationPayload },
    ) => {
      const data = "data" in payload ? payload.data : payload;
      useLiveScoreboardStore.getState().pushNotification(data);
    };
    socket.on("connect", onReady);
    socket.on("disconnect", onError);
    socket.on("connect_error", onError);
    socket.on("flag_stolen", onEvent);
    if (socket.connected) onReady();
    return () => {
      socket.off("connect", onReady);
      socket.off("disconnect", onError);
      socket.off("connect_error", onError);
      socket.off("flag_stolen", onEvent);
    };
  }, []);
  return <>{children}</>;
}
