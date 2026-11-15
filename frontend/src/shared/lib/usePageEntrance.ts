import { useLayoutEffect, useRef, type RefObject } from "react";
import { isMotionAllowed, watchMotionDisable } from "./motionPreference";

/** One short transition per route; live data never restarts it. */
export function usePageEntrance(
  pathname: string,
  keyboardNavigation: RefObject<boolean>,
) {
  const contentRef = useRef<HTMLDivElement>(null);

  useLayoutEffect(() => {
    const content = contentRef.current;
    const preference = window.matchMedia("(prefers-reduced-motion: reduce)");
    if (!content || keyboardNavigation.current || !isMotionAllowed(preference) ||
      typeof content.animate !== "function") {
      return;
    }

    // Keep the new page readable from its first frame. A tiny displacement
    // bridges the route change without moving individual rows or delaying data.
    const animation = content.animate([
      {
        opacity: 0.65,
        transform: "translateY(4px)"
      },
      {
        opacity: 1,
        transform: "translateY(0)"
      },
    ], {
      duration: 180,
      easing: getComputedStyle(document.documentElement).getPropertyValue("--ease-out").trim(),
      fill: "both",
    });
    animation.id = "page-content-enter";
    let stopWatching = () => {};
    const stop = () => {
      animation.onfinish = null;
      animation.cancel();
      stopWatching();
    };
    stopWatching = watchMotionDisable(preference, stop);
    animation.onfinish = stop;
    return stop;
  }, [pathname, keyboardNavigation]);

  return contentRef;
}
