import { RealtimeEvent } from "./generated/schemas/realtimeEvent.zod.ts";
import type { z } from "zod";

export type RealtimeEventMessage = z.infer<typeof RealtimeEvent>;

export function parseRealtimeEvent(value: unknown): RealtimeEventMessage | null {
  if (typeof value !== "string") return null;

  let decoded: unknown;
  try {
    decoded = JSON.parse(value);
  } catch {
    return null;
  }

  const result = RealtimeEvent.safeParse(decoded);
  return result.success ? result.data : null;
}
