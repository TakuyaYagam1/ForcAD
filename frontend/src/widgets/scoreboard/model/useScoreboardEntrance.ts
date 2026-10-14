import { useLayoutEffect, useRef } from "react";
import {
  isMotionAllowed,
  watchMotionDisable,
} from "@/shared/lib/motionPreference";

interface EntranceStep {
  element: Element | null;
  name: string;
  from: Keyframe;
  to: Keyframe;
  duration: number;
  delay?: number;
}

function playEntrance(steps: EntranceStep[]) {
  const preference = window.matchMedia("(prefers-reduced-motion: reduce)");
  if (!isMotionAllowed(preference)) return () => {};

  const animations = new Set<Animation>();
  let stopped = false;
  let stopWatchingMotion = () => {};

  const stop = () => {
    if (stopped) return;
    stopped = true;

    stopWatchingMotion();

    animations.forEach((animation) => animation.cancel());
    animations.clear();
  };

  stopWatchingMotion = watchMotionDisable(preference, stop);

  steps.forEach(({ element, name, from, to, duration, delay = 0 }) => {
    if (!element || typeof element.animate !== "function") return;

    const animation = element.animate([from, to], {
      duration,
      delay,
      easing: "cubic-bezier(.22,1,.36,1)",
      fill: "both",
    });

    animation.id = `scoreboard-enter-${name}`;
    animations.add(animation);

    animation.onfinish = () => {
      animations.delete(animation);

      // Release temporary clipping/transforms so sticky cells work normally.
      animation.cancel();

      if (!animations.size) stop();
    };
  });

  if (!animations.size) stop();

  return stop;
}

export function useScoreboardEntrance(
  enabled: boolean,
  filter: string,
  hasTeams: boolean,
  locale: string,
) {
  const pageRef = useRef<HTMLDivElement>(null);
  const tableRef = useRef<HTMLDivElement>(null);
  const startedAt = useRef(0);
  const query = filter.trim().toLocaleLowerCase(locale);

  useLayoutEffect(() => {
    if (!enabled || !pageRef.current) return;

    startedAt.current = performance.now();

    const page = pageRef.current;
    const art = page.querySelector<HTMLElement>(".hero-art");
    const artStyle = art ? getComputedStyle(art) : null;
    const artTransform = artStyle?.transform ?? "none";
    const artOpacity = artStyle?.opacity ?? "1";

    const step = (
      selector: string,
      name: string,
      from: Keyframe,
      to: Keyframe,
      duration: number,
      delay: number,
    ): EntranceStep => ({
      element: page.querySelector(selector),
      name,
      from,
      to,
      duration,
      delay,
    });

    return playEntrance([
      {
        element: art,
        name: "page-art",
        from: {
          opacity: 0,
          transform: `${artTransform === "none" ? "" : artTransform} translateY(8px) scale(.98)`,
        },
        to: {
          opacity: artOpacity,
          transform: artTransform,
        },
        duration: 1000,
      },
      step(
        ".hero-copy h1",
        "title",
        { opacity: 0, clipPath: "inset(0 100% 0 0)" },
        { opacity: 1, clipPath: "inset(0 0 0 0)" },
        800,
        120,
      ),
      ...[".hero-subtitle", ".hero-metadata"].map((selector, index) =>
        step(
          selector,
          `copy-${index}`,
          { opacity: 0, transform: "translateY(8px)" },
          { opacity: 1, transform: "translateY(0)" },
          450,
          220 + index * 40,
        ),
      ),
      step(
        ".round-track",
        "round-track",
        { transform: "scaleX(0)", transformOrigin: "left center" },
        { transform: "scaleX(1)", transformOrigin: "left center" },
        700,
        160,
      ),
      ...[".round-label", ".round-timer", ".scoreboard-toolbar"].map(
        (selector, index) =>
          step(
            selector,
            `controls-${index}`,
            { opacity: 0 },
            { opacity: 1 },
            350,
            180 + index * 50,
          ),
      ),
      ...[".recent-events", ".table-hint"].map((selector, index) =>
        step(
          selector,
          `details-${index}`,
          { opacity: 0, transform: "translateY(12px)" },
          { opacity: 1, transform: "translateY(0)" },
          400,
          650,
        ),
      ),
    ]);
  }, [enabled]);

  useLayoutEffect(() => {
    if (!enabled || !tableRef.current) return;

    // Delay only while the page is opening; filtering later starts immediately.
    const delay = Math.max(0, 320 - (performance.now() - startedAt.current));

    return playEntrance([
      {
        element: tableRef.current,
        name: "table",
        from: {
          opacity: 0,
          clipPath: "inset(0 0 100% 0)",
        },
        to: {
          opacity: 1,
          clipPath: "inset(0 0 0 0)",
        },
        duration: 600,
        delay,
      },
    ]);
  }, [enabled, query, hasTeams]);

  return { pageRef, tableRef };
}
