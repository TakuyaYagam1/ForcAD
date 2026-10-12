import assert from "node:assert/strict";
import test from "node:test";
import { parseRealtimeEvent } from "../src/shared/api/realtime.ts";
import { EventSocket, eventStreamUrl } from "../src/shared/lib/eventSocket.ts";

type TestEvent = "open" | "message" | "error" | "close";

class FakeSocket {
  private readonly listeners = new Map<TestEvent, Set<(event: unknown) => void>>();
  closed = false;

  addEventListener(type: string, listener: (event: unknown) => void) {
    const event = type as TestEvent;
    const handlers = this.listeners.get(event) ?? new Set();
    handlers.add(listener);
    this.listeners.set(event, handlers);
  }

  fire(type: TestEvent, event: unknown = {}) {
    for (const listener of this.listeners.get(type) ?? []) listener(event);
  }

  close() {
    this.closed = true;
    this.fire("close");
  }
}

test("event stream URL uses the native WebSocket scheme and endpoint", () => {
  assert.equal(
    eventStreamUrl("https://game.example"),
    "wss://game.example/api/events/ws",
  );
  assert.equal(
    eventStreamUrl("http://localhost:5173"),
    "ws://localhost:5173/api/events/ws",
  );
});

test("event facade dispatches validated payloads and supports off", () => {
  const sockets: FakeSocket[] = [];
  const socket = new EventSocket({
    url: "ws://localhost/api/events/ws",
    parseMessage: parseRealtimeEvent,
    createSocket: () => {
      const created = new FakeSocket();
      sockets.push(created);
      return created as unknown as WebSocket;
    },
    schedule: (() => 1) as typeof setTimeout,
    cancel: (() => {}) as typeof clearTimeout,
  });
  const received: unknown[] = [];
  const onFlag = (payload: unknown) => received.push(payload);
  socket.on("flag_stolen", onFlag).connect();
  sockets[0]!.fire("open");
  assert.equal(socket.connected, true);

  const event = {
    type: "flag_stolen",
    payload: {
      data: {
        attacker_id: "0199f7c0-0000-7000-8000-000000000003",
        victim_id: "0199f7c0-0000-7000-8000-000000000007",
        task_id: "0199f7c0-0000-7000-8000-000000000008",
        attacker_delta: 125,
        generation: 4,
      },
    },
    timestamp: "2026-10-11T12:00:00Z",
  };
  sockets[0]!.fire("message", { data: JSON.stringify(event) });
  assert.deepEqual(received, [event.payload]);

  socket.off("flag_stolen", onFlag);
  sockets[0]!.fire("message", { data: JSON.stringify(event) });
  assert.equal(received.length, 1);
  socket.disconnect();
  assert.equal(sockets[0]!.closed, true);
  assert.equal(socket.connected, false);
});

test("reconnect uses jitter and resynchronizes the full scoreboard", async () => {
  const sockets: FakeSocket[] = [];
  let scheduled: { callback: () => void; delay: number } | undefined;
  let cancelled = false;
  const scoreboard = {
    teams: [{ id: "0199f7c0-0000-7000-8000-000000000003" }],
    state: { round: 12 },
  };
  const socket = new EventSocket({
    url: "ws://localhost/api/events/ws",
    parseMessage: parseRealtimeEvent,
    resync: async () => scoreboard,
    createSocket: () => {
      const created = new FakeSocket();
      sockets.push(created);
      return created as unknown as WebSocket;
    },
    schedule: ((callback: TimerHandler, delay = 0) => {
      scheduled = { callback: callback as () => void, delay };
      return 1;
    }) as typeof setTimeout,
    cancel: (() => {
      cancelled = true;
    }) as typeof clearTimeout,
    random: () => 0.4,
    initialDelayMs: 500,
    maximumDelayMs: 2000,
  });
  const snapshots: unknown[] = [];
  socket.on("init_scoreboard", (payload) => snapshots.push(payload));
  socket.connect();
  sockets[0]!.fire("open");
  sockets[0]!.fire("close");

  assert.equal(scheduled?.delay, 450);
  scheduled?.callback();
  assert.equal(sockets.length, 2);
  sockets[1]!.fire("open");
  await Promise.resolve();

  assert.deepEqual(snapshots, [{ data: scoreboard }]);
  sockets[1]!.fire("close");
  socket.disconnect();
  assert.equal(cancelled, true);
});
