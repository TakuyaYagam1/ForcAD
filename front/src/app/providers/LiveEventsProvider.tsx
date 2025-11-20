import { type ReactNode, useEffect } from "react";
import { getLiveEventsSocket } from "@/shared/lib/socket";
import { useLiveScoreboardStore } from "@/entities/live-scoreboard/model/store";

interface Props {
  children: ReactNode;
}

export function LiveEventsProvider({ children }: Props) {
  const pushNotification = useLiveScoreboardStore((s) => s.pushNotification);
  const setError = useLiveScoreboardStore((s) => s.setError);

  useEffect(() => {
    const socket = getLiveEventsSocket();

    const handleFlagStolen = (payload: any) => {
      const data = payload?.data ?? payload;
      if (!data) return;
      pushNotification(data);
    };

    const handleConnectError = (err: any) => {
      console.error("live_events connect_error", err);
      setError("Live scoreboard connection error");
    };

    socket.on("flag_stolen", handleFlagStolen);
    socket.on("connect_error", handleConnectError);

    return () => {
      socket.off("flag_stolen", handleFlagStolen);
      socket.off("connect_error", handleConnectError);
    };
  }, [pushNotification, setError]);

  return <>{children}</>;
}
