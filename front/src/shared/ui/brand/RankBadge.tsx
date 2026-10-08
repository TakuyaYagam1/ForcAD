import { useId } from "react";
import "./RankBadge.css";

const leaves = [
  { x: 14, y: 6, angle: 10 },
  { x: 10, y: 11, angle: -10 },
  { x: 7, y: 16, angle: -30 },
  { x: 6, y: 21, angle: -50 },
  { x: 8, y: 26, angle: -65 },
  { x: 11, y: 31, angle: -80 },
  { x: 16, y: 35, angle: -100 },
];

export function RankBadge({ rank }: { rank: number }) {
  const gradientId = `rank-laurel-${useId()}`;
  const isMedal = rank >= 1 && rank <= 3;

  return (
    <span
      className={`rank-number rank-badge ${isMedal ? "rank-number--medal" : ""} rank-number--${rank}`}
    >
      {isMedal && (
        <svg
          className="rank-laurel"
          viewBox="-6 -6 72 50"
          preserveAspectRatio="xMidYMid meet"
          fill={`url(#${gradientId})`}
          aria-hidden="true"
        >
          <defs>
            <linearGradient id={gradientId} x1="0%" y1="0%" x2="100%" y2="100%">
              <stop offset="0%" stopColor="var(--rank-highlight)" />
              <stop offset="30%" stopColor="var(--rank-metal)" />
              <stop offset="55%" stopColor="var(--rank-highlight)" />
              <stop offset="80%" stopColor="var(--rank-metal)" />
              <stop offset="100%" stopColor="var(--rank-shadow)" />
            </linearGradient>
          </defs>

          {[false, true].map((mirror) => (
            <g
              key={String(mirror)}
              transform={mirror ? "translate(60 0) scale(-1 1)" : undefined}
            >
              <path
                d="M18 37C5 30 3 17 13 3"
                fill="none"
                stroke="currentColor"
                strokeWidth=".8"
              />

              {leaves.map((leaf) => (
                <g
                  key={leaf.y}
                  transform={`translate(${leaf.x} ${leaf.y}) rotate(${leaf.angle})`}
                >
                  <path d="M0 0C-5-2-6-6-5-9C-1-8 2-5 0 0ZM0 0C4-2 5-5 4-8C1-7-1-4 0 0Z" />
                  <path
                    d="M-4-7L0 0L3-6"
                    fill="none"
                    stroke="var(--rank-vein)"
                    strokeWidth=".5"
                  />
                </g>
              ))}
            </g>
          ))}
        </svg>
      )}

      <span className="rank-badge__value">{rank}</span>
    </span>
  );
}
