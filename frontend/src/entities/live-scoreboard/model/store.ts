import { create } from "zustand";
import { devtools } from "zustand/middleware";
import { useScoreboardStore } from "@/entities/scoreboard/model/store";

export interface LiveEvent {
  id: string;
  ts: number;
  attackerId: string;
  victimId: string;
  taskId: string;
  attackerName: string;
  victimName: string;
  taskName: string;
  delta: number;
}

export interface FlagNotificationPayload {
  generation?: number;
  attacker_id: string;
  victim_id: string;
  task_id: string;
  attacker_delta: number;
}

interface LiveScoreboardState {
  events: LiveEvent[];
  error: "connectionLost" | null;

  pushNotification: (payload: FlagNotificationPayload) => void;
  setError: (error: "connectionLost" | null) => void;
  clear: () => void;
}

export const useLiveScoreboardStore = create<LiveScoreboardState>()(
  devtools((set, get) => ({
    events: [],
    error: null,

    setError: (error) => set({ error }),
    clear: () => set({ events: [], error: null }),

    pushNotification: ({ attacker_id, victim_id, task_id, attacker_delta, generation }) => {
      const { events } = get();
      const { teams, tasks, generation: currentGeneration } = useScoreboardStore.getState();
      if ((generation ?? 0) !== currentGeneration) return;

      const attackerName =
        teams?.find((t) => t.id === attacker_id)?.name ?? `#${attacker_id}`;
      const victimName =
        teams?.find((t) => t.id === victim_id)?.name ?? `#${victim_id}`;
      const taskName =
        tasks?.find((t) => t.id === task_id)?.name ?? `task ${task_id}`;

      const now = Date.now();

      const next: LiveEvent = {
        id: crypto.randomUUID(),
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
      set({ events: newEvents, error: null });
    },
  })),
);

// Resolve names again when the scoreboard arrives after the event stream.
useScoreboardStore.subscribe((state, previous) => {
  if (state.generation !== previous.generation) {
    useLiveScoreboardStore.getState().clear();
    return;
  }
  if (state.teams === previous.teams && state.tasks === previous.tasks) return;
  useLiveScoreboardStore.setState((current) => ({
    events: current.events.map((event) => ({
      ...event,
      attackerName:
        state.teams?.find((team) => team.id === event.attackerId)?.name ??
        event.attackerName,
      victimName:
        state.teams?.find((team) => team.id === event.victimId)?.name ??
        event.victimName,
      taskName:
        state.tasks?.find((task) => task.id === event.taskId)?.name ??
        event.taskName,
    })),
  }));
});
