import { useTranslation } from "react-i18next";

export function TeamTaskLogWidget() {
  const { t } = useTranslation("admin");
  return <div>{t("widgets.teamTaskLog")}</div>;
}
