import { useEffect, type RefObject } from "react";

/** Pause decorative CSS animations once their whole region is off screen. */
export function useViewportAnimations<T extends HTMLElement>(
  ref: RefObject<T | null>,
  refreshKey?: unknown,
) {
  useEffect(() => {
    const element = ref.current;
    if (!element || typeof IntersectionObserver === "undefined") return;

    const observer = new IntersectionObserver(
      ([entry]) => {
        element.dataset.viewportAnimation = entry.isIntersecting
          ? "running"
          : "paused";
      },
      // Start animations shortly before the region reaches the viewport.
      { rootMargin: "160px 0px" },
    );

    observer.observe(element);

    return () => {
      observer.disconnect();
      delete element.dataset.viewportAnimation;
    };
  }, [ref, refreshKey]);
}
