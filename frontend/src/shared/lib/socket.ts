import { parseRealtimeEvent } from "@/shared/api/realtime";
import { SERVER_URL } from "@/app/config";
import { EventSocket, eventStreamUrl } from "./eventSocket";

let eventSocket: EventSocket | null = null;

function getEventSocket(): EventSocket {
  if (!eventSocket) {
    eventSocket = new EventSocket({
      url: eventStreamUrl(SERVER_URL),
      parseMessage: parseRealtimeEvent,
    });
    eventSocket.connect();
  }
  return eventSocket;
}

export function getGameEventsSocket(): EventSocket {
  return getEventSocket();
}

export function getLiveEventsSocket(): EventSocket {
  return getEventSocket();
}
