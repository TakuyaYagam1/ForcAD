import { useLayoutEffect, useRef, type RefObject } from "react";
import { isMotionAllowed, watchMotionDisable } from "@/shared/lib/motionPreference";
import { formatScore } from "@/shared/lib/formatScore";
import { diffScoreMotion, type ScoreMotionSnapshot } from "./scoreboardMotion";

function translationY(row: HTMLElement): number {
  const transform = getComputedStyle(row).transform;
  return transform === "none" ? 0 : new DOMMatrixReadOnly(transform).m42;
}

/** Keyed, interruptible row motion; the table always contains the current data. */
export function useScoreboardMotion(
  frameRef: RefObject<HTMLDivElement | null>,
  snapshot: ScoreMotionSnapshot,
) {
  const previous = useRef<ScoreMotionSnapshot | null>(null);
  const positions = useRef(new Map<string, number>());
  const animations = useRef(new Map<string, Animation>());
  const cancelRef = useRef(() => {});
  const signature = JSON.stringify(snapshot);

  useLayoutEffect(() => {
    const frame = frameRef.current;
    if (!frame) {
      return;
    }
    const preference = window.matchMedia("(prefers-reduced-motion: reduce)");
    const rows = () => {
      return Array.from(frame.querySelectorAll<HTMLElement>("tbody > [data-team-id]"))
    };
    const measure = () => {
      positions.current = new Map(rows().map((row) => {
        return [
        row.dataset.teamId!, row.getBoundingClientRect().top - frame.getBoundingClientRect().top - translationY(row),
        ]
      }));
    };
    const cancel = () => {
      animations.current.forEach((animation) => {
        return animation.cancel()
      });
      animations.current.clear();
      frame.querySelectorAll<HTMLElement>(".score-delta").forEach((delta) => {
        delta.textContent = "";
      });
      measure();
    };
    cancelRef.current = cancel;
    const stopWatching = watchMotionDisable(preference, cancel);
    frame.addEventListener("scroll", cancel, true);
    window.addEventListener("scroll", cancel, true);
    window.addEventListener("resize", cancel);
    const resizeObserver = new ResizeObserver(cancel);
    resizeObserver.observe(frame);
    rows().forEach((row) => {
      return resizeObserver.observe(row)
    });
    measure();
    return () => {
      stopWatching();
      frame.removeEventListener("scroll", cancel, true);
      window.removeEventListener("scroll", cancel, true);
      window.removeEventListener("resize", cancel);
      resizeObserver.disconnect();
      cancel();
    };
  }, [frameRef, snapshot.rows.length]);

  useLayoutEffect(() => {
    const current = JSON.parse(signature) as ScoreMotionSnapshot;
    const prior = previous.current;
    const changes = diffScoreMotion(prior, current);
    previous.current = current;
    const frame = frameRef.current;
    if (!frame) {
      return;
    }
    const preference = window.matchMedia("(prefers-reduced-motion: reduce)");
    const rows = Array.from(frame.querySelectorAll<HTMLElement>("tbody > [data-team-id]"));
    const basePositions = new Map<string, number>();
    const visualPositions = new Map<string, number>();
    rows.forEach((row) => {
      const id = row.dataset.teamId!;
      const translation = translationY(row);
      basePositions.set(id, row.getBoundingClientRect().top - frame.getBoundingClientRect().top - translation);
      visualPositions.set(id, (positions.current.get(id) ?? basePositions.get(id)!) + translation);
    });
    const reset = !prior || prior.scope !== current.scope || current.resetPending;
    if (reset || !isMotionAllowed(preference)) {
      cancelRef.current();
      positions.current = basePositions;
      return;
    }
    const tokens = getComputedStyle(document.documentElement);
    const easeOut = tokens.getPropertyValue("--ease-out").trim() || "cubic-bezier(0.23, 1, 0.32, 1)";
    const easeMove = tokens.getPropertyValue("--ease-in-out").trim() || "cubic-bezier(0.77, 0, 0.175, 1)";
    const play = (element: HTMLElement, id: string, frames: Keyframe[], duration: number, easing: string, done?: () => void) => {
      animations.current.get(id)?.cancel();
      if (typeof element.animate !== "function") {
        done?.();
        return;
      }
      const animation = element.animate(frames, {
        duration,
        easing,
        fill: "both"
      });
      animation.id = id;
      animations.current.set(id, animation);
      animation.onfinish = () => {
        if (animations.current.get(id) !== animation) {
          return;
        }
        animations.current.delete(id);
        animation.cancel();
        done?.();
      };
    };
    rows.forEach((row) => {
      const id = row.dataset.teamId!;
      const rect = row.getBoundingClientRect();
      const bounds = frame.getBoundingClientRect();
      if (rect.bottom <= 0 || rect.top >= window.innerHeight ||
        rect.bottom <= bounds.top || rect.top >= bounds.bottom) {
        for (const prefix of ["scoreboard-reorder", "score-delta", "leader-highlight"]) {
          const key = `${prefix}-${id}`;
          animations.current.get(key)?.cancel();
          animations.current.delete(key);
        }
        const slot = row.querySelector<HTMLElement>(".score-delta");
        if (slot) {
          slot.textContent = "";
        }
        return;
      }
      const rowAnimation = `scoreboard-reorder-${id}`;
      // Retarget an in-flight row from its actual visual position on every packet.
      if (changes.moved.has(id) || animations.current.has(rowAnimation)) {
        const distance = visualPositions.get(id)! - basePositions.get(id)!;
        if (Math.abs(distance) > 0.5) {
          play(row, rowAnimation, [
            {
              transform: `translateY(${distance}px)`
            },
            {
              transform: "translateY(0)"
            },
          ], 600, easeMove);
        } else {
          animations.current.get(rowAnimation)?.cancel();
          animations.current.delete(rowAnimation);
        }
      }
      const delta = changes.deltas.get(id);
      const slot = row.querySelector<HTMLElement>(".score-delta");
      if (delta !== undefined && slot) {
        slot.textContent = `${delta > 0 ? "+" : "−"}${formatScore(Math.abs(delta))}`;
        slot.dataset.direction = delta > 0 ? "up" : "down";
        play(slot, `score-delta-${id}`, [
          {
            transform: "translateY(8px)",
            opacity: 0,
            easing: easeOut
          },
          {
            transform: "translateY(0)",
            opacity: 1,
            offset: 0.18
          },
          {
            transform: "translateY(0)",
            opacity: 1,
            offset: 0.75,
            easing: easeOut
          },
          {
            transform: "translateY(-5px)",
            opacity: 0
          },
        ], 1000, "linear", () => {
          slot.textContent = "";
        });
      }
      if (changes.leader === id) {
        const outline = row.querySelector<HTMLElement>(".scoreboard-leader-outline");
        if (outline) {
          play(outline, `leader-highlight-${id}`, [
            {
              opacity: 0.9
            }, {
              opacity: 0.9,
              offset: 0.65,
              easing: easeOut
            }, {
              opacity: 0
            },
          ], 900, "linear");
        }
      }
    });
    positions.current = basePositions;
  }, [frameRef, signature]);
}
