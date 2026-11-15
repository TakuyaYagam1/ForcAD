import { useEffect, useRef, useState } from "react";
import { isMotionAllowed } from "@/shared/lib/motionPreference";

/** Keep the character grid readable, and run its cursor only while visible. */
export function useAsciiRoundTrack(running: boolean) {
  const trackRef = useRef<HTMLDivElement>(null);
  const [columns, setColumns] = useState(24);

  useEffect(() => {
    const track = trackRef.current;
    if (!track) {
      return;
    }
    const observer = new ResizeObserver(([entry]) => {
      // Include two bracket cells; 10.5px leaves room for a 16px mono glyph.
      setColumns(Math.max(8, Math.min(140, Math.floor(entry.contentRect.width / 10.5) - 2)));
    });
    observer.observe(track);
    return () => {
      return observer.disconnect()
    };
  }, []);

  useEffect(() => {
    const track = trackRef.current;
    if (!track) {
      return;
    }
    const preference = window.matchMedia("(prefers-reduced-motion: reduce)");
    let inViewport = false;
    const update = () => {
      track.dataset.cursor = running && inViewport && isMotionAllowed(preference) ? "running" : "stopped";
    };
    update();
    const intersectionObserver = new IntersectionObserver(([entry]) => {
      inViewport = entry.isIntersecting;
      update();
    });
    intersectionObserver.observe(track);
    preference.addEventListener("change", update);
    document.addEventListener("visibilitychange", update);
    const observer = new MutationObserver(update);
    observer.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ["data-motion"],
    });
    return () => {
      preference.removeEventListener("change", update);
      document.removeEventListener("visibilitychange", update);
      observer.disconnect();
      intersectionObserver.disconnect();
      track.dataset.cursor = "stopped";
    };
  }, [running]);

  return {
    trackRef,
    columns
  };
}
