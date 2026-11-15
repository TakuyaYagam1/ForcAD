import { useLayoutEffect, useRef } from "react";
import { useLocation, useNavigationType } from "react-router-dom";

interface ScrollPosition {
  left: number;
  top: number;
}

/** Restore history entries, and open newly selected pages at their heading. */
export function useRouteScroll() {
  const location = useLocation();
  const navigationType = useNavigationType();
  const positions = useRef(new Map<string, ScrollPosition>());
  const current = useRef<{
    key: string | null;
    position: ScrollPosition
  }>({
    key: null,
    position: {
      left: 0,
      top: 0
    }
  });

  useLayoutEffect(() => {
    const previousRestoration = window.history.scrollRestoration;
    window.history.scrollRestoration = "manual";
    const recordPosition = () => {
      current.current.position = {
        left: window.scrollX,
        top: window.scrollY
      };
    };
    window.addEventListener("scroll", recordPosition, {
      passive: true
    });
    // Capture the outgoing position before a navigation changes the DOM. A
    // shorter destination can clamp window.scrollY before layout effects run.
    document.addEventListener("pointerdown", recordPosition, true);
    document.addEventListener("keydown", recordPosition, true);
    return () => {
      window.history.scrollRestoration = previousRestoration;
      window.removeEventListener("scroll", recordPosition);
      document.removeEventListener("pointerdown", recordPosition, true);
      document.removeEventListener("keydown", recordPosition, true);
    };
  }, []);

  useLayoutEffect(() => {
    const previous = current.current;
    if (previous.key) {
      positions.current.set(previous.key, previous.position);
    }
    const position = navigationType === "POP"
      ? positions.current.get(location.key)
      : undefined;
    const target = position ?? {
      left: 0,
      top: 0
    };
    current.current = {
      key: location.key,
      position: target
    };
    // This hook runs before usePageEntrance in AppShell. Navigation never
    // flies through the previous page's scroll range or fades a hidden heading.
    window.scrollTo({
      ...target,
      behavior: "instant"
    });
  }, [location.key, navigationType]);
}
