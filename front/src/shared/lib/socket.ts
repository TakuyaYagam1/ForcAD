import { io, Socket } from "socket.io-client";
import { SERVER_URL } from "@/app/config";

let gameEventsSocket: Socket | null = null;
let liveEventsSocket: Socket | null = null;

// Both namespaces share one transport, leaving server threads for new clients.
export function getGameEventsSocket(): Socket {
  if (!gameEventsSocket) {
    gameEventsSocket = io(`${SERVER_URL}/game_events`, {
      transports: ["polling", "websocket"],
    });
  }
  return gameEventsSocket;
}

export function getLiveEventsSocket(): Socket {
  if (!liveEventsSocket) {
    liveEventsSocket = io(`${SERVER_URL}/live_events`, {
      transports: ["polling", "websocket"],
    });
  }
  return liveEventsSocket;
}
