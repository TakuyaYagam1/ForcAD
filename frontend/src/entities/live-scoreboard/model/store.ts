import { create } from "zustand";
import { devtools } from "zustand/middleware";
import { useScoreboardStore } from "@/entities/scoreboard/model/store";
import {
  mergeLiveEvents,
  notificationToLiveEvent,
  publicEventToLiveEvent,
  resolveEventNames,
  type FlagNotification,
  type LiveEvent,
  type PublicEventRecord,
} from "./events";

export type { LiveEvent } from "./events";
export type { FlagNotification as FlagNotificationPayload } from "./events";

interface LiveScoreboardState {
  events: LiveEvent[];
  error: "connectionLost" | null;

  hydrateEvents: (feed: PublicEventRecord[], replace?: boolean) => void;
  pushNotification: (payload: FlagNotification) => void;
  setError: (error: "connectionLost" | null) => void;
  clear: () => void;
}

export const useLiveScoreboardStore = create<LiveScoreboardState>()(
  devtools((set) => ({
    events: [],
    error: null,

    setError: (error) => set({ error }),
    clear: () => set({ events: [], error: null }),

    hydrateEvents: (feed, replace = false) => {
      const incoming = feed
        .map(publicEventToLiveEvent)
        .filter((event): event is LiveEvent => event !== null);
      set((state) => ({
        events: mergeLiveEvents(replace ? [] : state.events, incoming),
        error: null,
      }));
    },

    pushNotification: (payload) => {
      const {
        generation: currentGeneration,
        teams,
        tasks,
      } = useScoreboardStore.getState();
      if ((payload.generation ?? currentGeneration) !== currentGeneration) return;
      const event = resolveEventNames(notificationToLiveEvent(payload), teams, tasks);
      set((state) => ({
        events: mergeLiveEvents(state.events, [event]),
        error: null,
      }));
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
    events: current.events.map((event) =>
      resolveEventNames(event, state.teams, state.tasks),
    ),
  }));
});
