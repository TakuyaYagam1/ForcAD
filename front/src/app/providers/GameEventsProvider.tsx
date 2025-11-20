import { useEffect, type ReactNode } from "react";
import { getGameEventsSocket } from "@/shared/lib/socket";
import { useScoreboardStore } from "@/entities/scoreboard/model/store";

interface Props {
  children: ReactNode;
}

export function GameEventsProvider({ children }: Props) {
  const handleInitScoreboardMessage = useScoreboardStore(
    (s) => s.handleInitScoreboardMessage
  );
  const handleUpdateScoreboardMessage = useScoreboardStore(
    (s) => s.handleUpdateScoreboardMessage
  );
  const setError = useScoreboardStore((s) => s.setError);

  useEffect(() => {
    const socket = getGameEventsSocket();

    let connectionErrors = 0;

    socket.on("connect_error", (err) => {
      // переключение транспорта как в Vue
      socket.io.opts.transports = ["polling", "websockets"];
      if (connectionErrors > 0) {
        console.error("Connection error:", err.message);
        setError("Can't connect to server");
      }
      connectionErrors += 1;
    });

    socket.on("init_scoreboard", ({ data }) => {
      setError(null);
      handleInitScoreboardMessage(data);
    });

    socket.on("update_scoreboard", ({ data }) => {
      setError(null);
      handleUpdateScoreboardMessage(data);
    });

    return () => {
      socket.off("connect_error");
      socket.off("init_scoreboard");
      socket.off("update_scoreboard");
      // socket.close(); // если хочешь закрывать сокет при unmount
    };
  }, [handleInitScoreboardMessage, handleUpdateScoreboardMessage, setError]);

  return <>{children}</>;
}
