import { useLayoutEffect, useRef } from "react";

export function usePageEntrance(pathname: string, navigationKey: string) {
  const mainRef = useRef<HTMLElement>(null);

  useLayoutEffect(() => {
    const kind =
      pathname === "/teams"
        ? "teams"
        : pathname === "/live"
          ? "events"
          : /^\/team\/[^/]+\/?$/.test(pathname)
            ? "team"
            : null;

    const main = mainRef.current;
    if (!kind || !main) return;

    const preference = window.matchMedia("(prefers-reduced-motion: reduce)");

    const motionAllowed = () => {
      if (
        preference.matches ||
        document.documentElement.dataset.motion === "off" ||
        document.visibilityState === "hidden"
      ) {
        return false;
      }

      try {
        return localStorage.getItem("cf-motion") !== "off";
      } catch {
        return true;
      }
    };

    if (!motionAllowed()) return;

    const played = new Set<string>();
    const animations = new Set<Animation>();
    let frame = 0;
    let stopped = false;

    const reveal = (
      name: string,
      selector: string,
      delay: number,
      stagger = 0,
      duration = 460,
      from: Keyframe = { opacity: 0, transform: "translateY(18px)" },
      to: Keyframe = { opacity: 1, transform: "none" },
    ) => {
      if (played.has(name)) return;

      const elements = Array.from(main.querySelectorAll<HTMLElement>(selector));
      if (!elements.length) return;

      played.add(name);

      elements.slice(0, 18).forEach((element, index) => {
        if (typeof element.animate !== "function") return;

        const animation = element.animate([from, to], {
          duration,
          delay: delay + Math.min(index, 8) * stagger,
          easing: "cubic-bezier(.22,1,.36,1)",
          fill: "both",
        });

        animation.id = `page-enter-${kind}-${name}-${index}`;
        animations.add(animation);

        animation.onfinish = () => {
          animations.delete(animation);
          animation.cancel();
        };
      });
    };

    const run = () => {
      frame = 0;
      if (stopped) return;

      if (!motionAllowed()) {
        stop();
        return;
      }

      const art = main.querySelector<HTMLElement>(".hero-art");

      if (art && !played.has("art")) {
        const style = window.getComputedStyle(art);
        const base = style.transform === "none" ? "" : style.transform;

        reveal(
          "art",
          ".hero-art",
          0,
          0,
          950,
          { opacity: 0, transform: `${base} rotate(-6deg) scale(.96)` },
          { opacity: style.opacity, transform: style.transform },
        );
      }

      reveal(
        "title",
        ".hero-copy h1, .team-header h1",
        110,
        0,
        720,
        { opacity: 0, clipPath: "inset(0 100% 0 0)" },
        { opacity: 1, clipPath: "inset(0 0 0 0)" },
      );

      reveal(
        "subtitle",
        ".hero-subtitle, .hero-metadata, .team-header-meta",
        190,
        45,
      );

      reveal("back", ".back-link", 80);
      reveal("toolbar", ".events-toolbar", 240);
      reveal("empty", ".empty-state", 280);

      if (kind === "teams") {
        reveal(
          "headings",
          ".teams-leaders > .section-heading, .teams-roster > .section-heading",
          260,
          80,
        );

        reveal("podium", ".teams-podium > .team-card", 300, 90, 620, {
          opacity: 0,
          transform: "translateY(28px) scale(.97)",
        });

        reveal("laurels", ".team-medallion .rank-laurel", 420, 90, 850, {
          opacity: 0,
          transform: "rotate(-7deg) scale(.9)",
        });

        reveal("cards", ".teams-grid > .team-card", 440, 55);
      } else if (kind === "events") {
        reveal("events", ".events-list > .event-item", 320, 45);
        reveal("hint", ".table-hint", 650);
      } else {
        reveal("avatar", ".team-header-identity > .team-avatar", 180, 0, 550, {
          opacity: 0,
          transform: "scale(.9)",
        });

        reveal("rank", ".team-current-rank", 240);
        reveal("stats", ".stats-grid > .stat-card", 280, 70, 520);

        reveal(
          "panels",
          ".services-panel, .history-panel, .team-content-aside",
          500,
          85,
          560,
        );
      }
    };

    const schedule = () => {
      if (!stopped && !frame) frame = window.requestAnimationFrame(run);
    };

    const checkMotion = () => {
      if (!motionAllowed()) stop();
    };

    const observer = new MutationObserver(schedule);
    const motionObserver = new MutationObserver(checkMotion);

    const stop = () => {
      if (stopped) return;

      stopped = true;

      if (frame) window.cancelAnimationFrame(frame);

      observer.disconnect();
      motionObserver.disconnect();

      preference.removeEventListener("change", checkMotion);
      document.removeEventListener("visibilitychange", checkMotion);

      animations.forEach((animation) => animation.cancel());
      animations.clear();
    };

    observer.observe(main, { childList: true, subtree: true });

    motionObserver.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ["data-motion"],
    });

    preference.addEventListener("change", checkMotion);
    document.addEventListener("visibilitychange", checkMotion);

    run();

    return stop;
  }, [pathname, navigationKey]);

  return mainRef;
}
