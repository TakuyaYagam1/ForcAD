import type { PublicEventOutput } from "../../../shared/api/generated/schemas/publicEvent.zod.ts";

export interface FlagLiveEvent {
  id: string;
  ts: number;
  kind?: "flag_stolen";
  attackerId: string;
  victimId: string;
  taskId: string;
  attackerName: string;
  victimName: string;
  taskName: string;
  delta?: number;
}

export interface AnnouncementLiveEvent {
  id: string;
  ts: number;
  kind: "admin_message";
  title: string;
  markdown: string;
  author?: string;
}

export type LiveEvent = FlagLiveEvent | AnnouncementLiveEvent;

export type PublicEventRecord = PublicEventOutput;

export interface FlagNotification {
  event_id?: string;
  created_at?: string;
  attacker_id: string;
  victim_id: string;
  task_id: string;
  attacker_delta: number;
  generation?: number;
}

export function publicEventToLiveEvent(event: PublicEventRecord): LiveEvent | null {
  const ts = Date.parse(event.created_at);
  if (!event.id || !Number.isFinite(ts)) return null;
  if (event.kind === "admin_message") {
    return {
      id: event.id,
      ts,
      kind: "admin_message",
      title: event.title ?? "",
      markdown: event.markdown ?? "",
      author: event.author,
    };
  }
  if (
    event.kind !== "flag_stolen" ||
    !event.attacker_id ||
    !event.victim_id ||
    !event.task_id
  ) {
    return null;
  }
  return {
    id: event.id,
    ts,
    kind: "flag_stolen",
    attackerId: event.attacker_id,
    victimId: event.victim_id,
    taskId: event.task_id,
    attackerName: event.attacker_name ?? `#${event.attacker_id}`,
    victimName: event.victim_name ?? `#${event.victim_id}`,
    taskName: event.task_name ?? `task ${event.task_id}`,
    delta: event.attacker_delta,
  };
}

export function notificationToLiveEvent(
  payload: FlagNotification,
  now = Date.now(),
): FlagLiveEvent {
  const ts = payload.created_at ? Date.parse(payload.created_at) : now;
  return {
    id: payload.event_id ?? crypto.randomUUID(),
    ts: Number.isFinite(ts) ? ts : now,
    kind: "flag_stolen",
    attackerId: payload.attacker_id,
    victimId: payload.victim_id,
    taskId: payload.task_id,
    attackerName: `#${payload.attacker_id}`,
    victimName: `#${payload.victim_id}`,
    taskName: `task ${payload.task_id}`,
    delta: payload.attacker_delta,
  };
}

export function mergeLiveEvents(current: LiveEvent[], incoming: LiveEvent[]): LiveEvent[] {
  const byID = new Map(current.map((event) => [event.id, event]));
  for (const event of incoming) {
    const previous = byID.get(event.id);
    if (
      previous &&
      previous.kind !== "admin_message" &&
      event.kind !== "admin_message" &&
      event.delta === undefined &&
      previous.delta !== undefined
    ) {
      byID.set(event.id, { ...previous, ...event, delta: previous.delta });
      continue;
    }
    byID.set(event.id, event);
  }
  return [...byID.values()]
    .sort((a, b) => b.ts - a.ts || b.id.localeCompare(a.id))
    .slice(0, 100);
}
