import { useTranslation } from "react-i18next";
import { formatScore } from "@/shared/lib/formatScore";
import "./RankBadge.css";

export function RankBadge({ rank }: { rank: number }) {
  const { t } = useTranslation("common");

  return (
    <span
      className={`rank-number rank-badge rank-number--${rank}`}
      aria-label={t("rank.ariaLabel", { rank: formatScore(rank) })}
    >
      <span className="rank-badge__value">{formatScore(rank)}</span>
    </span>
  );
}
