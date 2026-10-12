import type { RealtimeEventMessage } from "../api/realtime.ts";
import type { Scoreboard } from "../api/generated/schemas/client/scoreboard.zod.ts";

type PublicEvent = RealtimeEventMessage["type"];
type EventData<T extends PublicEvent> = Extract<RealtimeEventMessage, { type: T }>["payload"];

interface SocketEventMap {
  connect: undefined;
  disconnect: undefined;
  connect_error: Error;
  init_scoreboard: EventData<"init_scoreboard">;
  update_scoreboard: EventData<"update_scoreboard">;
  flag_stolen: EventData<"flag_stolen">;
}

type SocketEvent = keyof SocketEventMap;
type SocketHandler<Event extends SocketEvent> = SocketEventMap[Event] extends undefined
  ? () => void
  : (payload: SocketEventMap[Event]) => void;
type EventParser = (value: unknown) => RealtimeEventMessage | null;

interface EventSocketOptions {
  url: string;
  parseMessage: EventParser;
  resync?: () => Promise<Scoreboard>;
  createSocket?: (url: string) => WebSocket;
  schedule?: typeof setTimeout;
  cancel?: typeof clearTimeout;
  random?: () => number;
  initialDelayMs?: number;
  maximumDelayMs?: number;
}

export class EventSocket {
  private readonly options: EventSocketOptions;
  private readonly listeners = new Map<SocketEvent, Set<(payload?: unknown) => void>>();
  private readonly createSocket: (url: string) => WebSocket;
  private readonly schedule: typeof setTimeout;
  private readonly cancel: typeof clearTimeout;
  private readonly random: () => number;
  private readonly initialDelayMs: number;
  private readonly maximumDelayMs: number;
  private socket: WebSocket | null = null;
  private reconnectTimer: ReturnType<typeof setTimeout> | undefined;
  private reconnectAttempt = 0;
  private hasConnected = false;
  private isConnected = false;
  private manuallyDisconnected = false;

  constructor(options: EventSocketOptions) {
    this.options = options;
    this.createSocket = options.createSocket ?? ((url) => new WebSocket(url));
    this.schedule = options.schedule ?? globalThis.setTimeout.bind(globalThis);
    this.cancel = options.cancel ?? globalThis.clearTimeout.bind(globalThis);
    this.random = options.random ?? Math.random;
    this.initialDelayMs = options.initialDelayMs ?? 500;
    this.maximumDelayMs = options.maximumDelayMs ?? 30_000;
  }

  get connected(): boolean {
    return this.isConnected;
  }

  on<Event extends SocketEvent>(event: Event, handler: SocketHandler<Event>): this {
    const handlers = this.listeners.get(event) ?? new Set();
    handlers.add(handler as (payload?: unknown) => void);
    this.listeners.set(event, handlers);
    return this;
  }

  off<Event extends SocketEvent>(event: Event, handler: SocketHandler<Event>): this {
    this.listeners.get(event)?.delete(handler as (payload?: unknown) => void);
    return this;
  }

  connect(): this {
    this.manuallyDisconnected = false;
    if (this.socket) return this;
    if (this.reconnectTimer !== undefined) {
      this.cancel(this.reconnectTimer);
      this.reconnectTimer = undefined;
    }

    const socket = this.createSocket(this.options.url);
    this.socket = socket;
    socket.addEventListener("open", () => this.handleOpen(socket));
    socket.addEventListener("message", (event) => this.handleMessage(socket, event));
    socket.addEventListener("error", () => this.handleError(socket));
    socket.addEventListener("close", () => this.handleClose(socket));
    return this;
  }

  disconnect(): this {
    this.manuallyDisconnected = true;
    if (this.reconnectTimer !== undefined) {
      this.cancel(this.reconnectTimer);
      this.reconnectTimer = undefined;
    }

    const socket = this.socket;
    this.socket = null;
    if (socket) socket.close(1000, "Client disconnected");
    if (this.isConnected) {
      this.isConnected = false;
      this.dispatch("disconnect");
    }
    return this;
  }

  private handleOpen(socket: WebSocket): void {
    if (this.socket !== socket) return;

    const reconnect = this.hasConnected;
    this.hasConnected = true;
    this.isConnected = true;
    this.reconnectAttempt = 0;
    this.dispatch("connect");

    if (reconnect && this.options.resync) {
      void this.options.resync().then(
        (scoreboard) => this.dispatch("init_scoreboard", { data: scoreboard }),
        (error: unknown) =>
          this.dispatch(
            "connect_error",
            error instanceof Error ? error : new Error("Scoreboard resync failed."),
          ),
      );
    }
  }

  private handleMessage(socket: WebSocket, event: MessageEvent): void {
    if (this.socket !== socket) return;
    const message = this.options.parseMessage(event.data);
    if (message) this.dispatch(message.type, message.payload);
  }

  private handleError(socket: WebSocket): void {
    if (this.socket === socket && !this.isConnected) {
      this.dispatch("connect_error", new Error("Event stream connection failed."));
    }
  }

  private handleClose(socket: WebSocket): void {
    if (this.socket !== socket) return;
    this.socket = null;
    if (this.isConnected) {
      this.isConnected = false;
      this.dispatch("disconnect");
    }
    if (!this.manuallyDisconnected) this.scheduleReconnect();
  }

  private scheduleReconnect(): void {
    const baseDelay = Math.min(
      this.maximumDelayMs,
      this.initialDelayMs * 2 ** this.reconnectAttempt,
    );
    const jitteredDelay = Math.min(
      this.maximumDelayMs,
      Math.floor(baseDelay * (0.5 + this.random())),
    );
    this.reconnectAttempt += 1;
    this.reconnectTimer = this.schedule(() => {
      this.reconnectTimer = undefined;
      this.connect();
    }, jitteredDelay);
  }

  private dispatch<Event extends SocketEvent>(
    event: Event,
    payload?: SocketEventMap[Event],
  ): void {
    const handlers = this.listeners.get(event);
    if (!handlers) return;
    for (const handler of handlers) {
      if (payload === undefined) handler();
      else handler(payload);
    }
  }
}

export function eventStreamUrl(origin: string): string {
  const url = new URL("/api/events/ws", origin);
  url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
  return url.toString();
}
