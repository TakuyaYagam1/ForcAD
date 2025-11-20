import { io, Socket } from "socket.io-client";
import { SERVER_URL } from "@/app/config";

let gameEventsSocket: Socket | null = null;
let liveEventsSocket: Socket | null = null;

export function getGameEventsSocket(): Socket {
  if (!gameEventsSocket) {
    gameEventsSocket = io(`${SERVER_URL}/game_events`, {
      forceNew: true,
      transports: ["websocket", "polling"],
    });
  }
  return gameEventsSocket;
}

export function getLiveEventsSocket(): Socket {
  if (!liveEventsSocket) {
    liveEventsSocket = io(`${SERVER_URL}/live_events`, {
      forceNew: true,
      transports: ["websocket", "polling"],
    });
  }
  return liveEventsSocket;
}
