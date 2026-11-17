import { useLayoutEffect, useRef, type RefObject } from "react";
import { isMotionAllowed, watchMotionDisable } from "@/shared/lib/motionPreference";
import { changedScoreboardRows, type ScoreboardSnapshot } from "./scoreboardUpdates";

/** A short signal in the row gutter; the ranking and readable data stay still. */
export function useScoreboardUpdates(
  frameRef: RefObject<HTMLDivElement | null>,
  snapshot: ScoreboardSnapshot,
) {
  const previous = useRef<ScoreboardSnapshot | null>(null);
  // A value dependency is intentional: identical socket payloads must not cancel
  // or restart an in-flight signal, even when their arrays are new objects.
  const signature = JSON.stringify(snapshot);

  useLayoutEffect(() => {
    const current = JSON.parse(signature) as ScoreboardSnapshot;
    const changed = new Set(changedScoreboardRows(previous.current, current));
    previous.current = current;
    const frame = frameRef.current;
    const preference = window.matchMedia("(prefers-reduced-motion: reduce)");
    if (!changed.size || !frame || !isMotionAllowed(preference)) {
      return;
    }

    const bounds = frame.getBoundingClientRect();
    if (bounds.bottom <= 0 || bounds.top >= window.innerHeight) {
      return;
    }
    const layer = document.createElement("div");
    layer.className = "scoreboard-update-layer";
    layer.setAttribute("aria-hidden", "true");
    const animations = new Set<Animation>();
    let stopped = false;
    let stopWatching = () => {};

    const stop = () => {
      if (stopped) {
        return;
      }
      stopped = true;
      stopWatching();
      frame.removeEventListener("scroll", stop, true);
      window.removeEventListener("resize", stop);
      animations.forEach((animation) => {
        return animation.cancel()
      });
      animations.clear();
      layer.remove();
    };

    const rows = Array.from(frame.querySelectorAll<HTMLElement>("tbody > .scoreboard-row[data-team-id]"));
    rows.filter((row) => {
      return changed.has(row.dataset.teamId ?? "")
    })
      .forEach((row) => {
        const rect = row.getBoundingClientRect();
        if (rect.bottom <= 0 || rect.top >= window.innerHeight ||
          rect.bottom <= bounds.top || rect.top >= bounds.bottom) {
          return;
        }

        const rail = document.createElement("span");
        rail.className = "scoreboard-update-rail";
        rail.dataset.teamId = row.dataset.teamId;
        rail.style.top = `${rect.bottom - bounds.top - 11}px`;
        layer.append(rail);

        // Sixteen squares travel through a dedicated lower gutter in <300ms.
        for (let index = 0; index < 16; index += 1) {
          const square = document.createElement("i");
          square.className = "scoreboard-update-square";
          rail.append(square);
          if (typeof square.animate !== "function") {
            continue;
          }
          const animation = square.animate([
            {
              opacity: 0,
              transform: "translateY(1px)"
            },
            {
              opacity: 0.28,
              transform: "translateY(0)",
              offset: 0.35
            },
            {
              opacity: 0,
              transform: "translateY(0)"
            },
          ], {
            duration: 220,
            delay: index * 4,
            easing: "cubic-bezier(0.23, 1, 0.32, 1)",
            fill: "both",
          });
          animation.id = `scoreboard-update-${row.dataset.teamId}-${index}`;
          animations.add(animation);
          animation.onfinish = () => {
            animations.delete(animation);
            animation.cancel();
            if (!animations.size) {
              stop();
            }
          };
        }
      });

    if (!animations.size) {
      stop();
      return;
    }
    frame.append(layer);
    frame.addEventListener("scroll", stop, true);
    window.addEventListener("resize", stop);
    stopWatching = watchMotionDisable(preference, stop);
    return stop;
  }, [frameRef, signature]);
}
