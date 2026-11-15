import { useTranslation } from "react-i18next";

import { formatScore } from "@/shared/lib/formatScore";

export function ScoreValue({ value }: {
  value: number
}) {
  const { t } = useTranslation("common");
  const score = Number.isFinite(value) ? Math.round(value) : 0;

  return (
    <span
      className="score-value"
      aria-label={t("score.ariaLabel", {
        value: formatScore(score)
      })}
    >
      {formatScore(score)}
    </span>
  );
}
