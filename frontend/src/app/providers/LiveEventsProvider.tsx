import { useEffect, type ReactNode } from "react";
import axios from "axios";
import { DEMO_MODE } from "@/app/demo/mode";
import { getLiveEventsSocket } from "@/shared/lib/socket";
import { useScoreboardStore } from "@/entities/scoreboard/model/store";
import { getClientEvents } from "@/shared/api/generated/endpoints/client/client";
import { PublicEvent } from "@/shared/api/generated/schemas/publicEvent.zod";
import {
  useLiveScoreboardStore,
  type FlagNotificationPayload,
} from "@/entities/live-scoreboard/model/store";
import type { PublicEventRecord } from "@/entities/live-scoreboard/model/events";

const eventPageSize = 100;
const maximumCatchUpPages = 10;

async function getEventPage(after?: string): Promise<PublicEventRecord[] | null> {
  try {
    const events = await getClientEvents(after ? { after } : undefined);
    return events.flatMap((event) => {
      const parsed = PublicEvent.safeParse(event);
      return parsed.success ? [parsed.data] : [];
    });
  } catch (error) {
    if (after && axios.isAxiosError(error) && error.response?.status === 400) return null;
    throw error;
  }
}

export function LiveEventsProvider({ children }: { children: ReactNode }) {
  useEffect(() => {
    if (DEMO_MODE) return;

    const socket = getLiveEventsSocket();
    let active = true;
    let feedRevision = 0;

    const syncFeed = async () => {
      const revision = ++feedRevision;
      const isCurrent = () => active && revision === feedRevision;
      try {
        const cursor = useLiveScoreboardStore.getState().events[0]?.id;
        const latest = await getEventPage();
        if (!isCurrent() || !latest) return;
        useLiveScoreboardStore.getState().hydrateEvents(latest);
        if (!cursor) return;
        let after = cursor;
        for (let page = 0; page < maximumCatchUpPages; page += 1) {
          const events = await getEventPage(after);
          if (!isCurrent()) return;
          if (events === null) {
            useLiveScoreboardStore.getState().clear();
            const reset = await getEventPage();
            if (isCurrent() && reset) {
              useLiveScoreboardStore.getState().hydrateEvents(reset);
            }
            return;
          }
          useLiveScoreboardStore.getState().hydrateEvents(events);
          if (events.length < eventPageSize) return;
          const newest = events.at(-1)?.id;
          if (!newest || newest === after) return;
          after = newest;
        }
      } catch {
        if (isCurrent()) useLiveScoreboardStore.getState().setError("connectionLost");
      }
    };

    const onReady = () => {
      useLiveScoreboardStore.getState().setError(null);
      void syncFeed();
    };
    const onError = () => {
      feedRevision += 1;
      useLiveScoreboardStore.getState().setError("connectionLost");
    };
    const onEvent = (
      payload: FlagNotificationPayload | { data: FlagNotificationPayload },
    ) => {
      const data = "data" in payload ? payload.data : payload;
      useLiveScoreboardStore.getState().pushNotification(data);
    };
    const onAnnouncement = (payload: { data: PublicEventRecord }) => {
      useLiveScoreboardStore.getState().hydrateEvents([payload.data]);
    };

    socket.on("connect", onReady);
    socket.on("disconnect", onError);
    socket.on("connect_error", onError);
    socket.on("flag_stolen", onEvent);
    socket.on("admin_message", onAnnouncement);
    if (socket.connected) onReady();
    const unsubscribeScoreboard = useScoreboardStore.subscribe((state, previous) => {
      if (state.generation !== previous.generation && socket.connected) void syncFeed();
    });
    return () => {
      active = false;
      feedRevision += 1;
      unsubscribeScoreboard();
      socket.off("connect", onReady);
      socket.off("disconnect", onError);
      socket.off("connect_error", onError);
      socket.off("flag_stolen", onEvent);
      socket.off("admin_message", onAnnouncement);
    };
  }, []);
  return <>{children}</>;
}
