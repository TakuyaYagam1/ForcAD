import { useEffect, useRef, useState } from "react";

import { formatScore } from "@/shared/lib/formatScore";

export function ScoreValue({ value }: { value: number }) {
  const score = Number.isFinite(value) ? Math.round(value) : 0;
  const [display, setDisplay] = useState(score);
  const previous = useRef(score);

  useEffect(() => {
    const from = previous.current;
    previous.current = score;
    const start = performance.now();

    const reduced =
      window.matchMedia("(prefers-reduced-motion: reduce)").matches ||
      document.documentElement.dataset.motion === "off";

    let frame: number;

    const tick = (now: number) => {
      const progress = reduced ? 1 : Math.min((now - start) / 550, 1);
      const eased = 1 - (1 - progress) ** 3;

      setDisplay(Math.round(from + (score - from) * eased));

      if (progress < 1) {
        frame = requestAnimationFrame(tick);
      }
    };

    frame = requestAnimationFrame(tick);

    return () => cancelAnimationFrame(frame);
  }, [score]);

  return (
    <span className="score-value" aria-label={formatScore(score)}>
      {formatScore(display)}
    </span>
  );
}
