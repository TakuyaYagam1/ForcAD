import { useTranslation } from "react-i18next";

export function AdminScoreboardWidget() {
  const { t } = useTranslation("admin");
  return <div>{t("widgets.scoreboard")}</div>;
}
