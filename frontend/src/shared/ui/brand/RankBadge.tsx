import "./RankBadge.css";

export function RankBadge({ rank }: { rank: number }) {
  return (
    <span
      className={`rank-number rank-badge rank-number--${rank}`}
      aria-label={`Место ${rank}`}
    >
      <span className="rank-badge__value">{rank}</span>
    </span>
  );
}
