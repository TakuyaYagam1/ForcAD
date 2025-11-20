import { create } from "zustand";
import { devtools } from "zustand/middleware";
import { useScoreboardStore } from "@/entities/scoreboard/model/store";

export interface LiveEvent {
  id: number;
  ts: number;
  attackerId: number;
  victimId: number;
  taskId: number;
  attackerName: string;
  victimName: string;
  taskName: string;
  delta: number;
}

export interface FlagNotificationPayload {
  attacker_id: number;
  victim_id: number;
  task_id: number;
  attacker_delta: number;
}

interface LiveScoreboardState {
  events: LiveEvent[];
  error: string | null;

  pushNotification: (payload: FlagNotificationPayload) => void;
  setError: (error: string | null) => void;
  clear: () => void;
}

export const useLiveScoreboardStore = create<LiveScoreboardState>()(
  devtools((set, get) => ({
    events: [],
    error: null,

    setError: (error) => set({ error }),
    clear: () => set({ events: [], error: null }),

    pushNotification: ({ attacker_id, victim_id, task_id, attacker_delta }) => {
      const { events } = get();
      const { teams, tasks } = useScoreboardStore.getState();

      const attackerName =
        teams?.find((t) => t.id === attacker_id)?.name ?? `#${attacker_id}`;
      const victimName =
        teams?.find((t) => t.id === victim_id)?.name ?? `#${victim_id}`;
      const taskName =
        tasks?.find((t) => t.id === task_id)?.name ?? `task ${task_id}`;

      const now = Date.now();

      const next: LiveEvent = {
        id: now + events.length,
        ts: now,
        attackerId: attacker_id,
        victimId: victim_id,
        taskId: task_id,
        attackerName,
        victimName,
        taskName,
        delta: attacker_delta,
      };

      const newEvents = [next, ...events].slice(0, 100);
      set({ events: newEvents });
    },
  }))
);
