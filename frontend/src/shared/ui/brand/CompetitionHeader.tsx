import { useScoreboardStore } from "@/entities/scoreboard/model/store";
import { useTranslation } from "react-i18next";
import { PageHero } from "./PageHero";

export function CompetitionHeader({ title, subtitle }: {
  title: string;
  subtitle?: string
}) {
  const { t } = useTranslation("public");
  const teams = useScoreboardStore((state) => {
    return state.teams
  });
  const tasks = useScoreboardStore((state) => {
    return state.tasks
  });
  const round = useScoreboardStore((state) => {
    return state.runtimeRound ?? state.round
  });
  return (
    <PageHero title={title} subtitle={subtitle} eyebrow="ForcAD / Attack–defence">
      <dl className="hero-register">
        <div><dt>{t("teams.title")}</dt><dd>{teams === null ? "—" : String(teams.length).padStart(2, "0")}</dd></div>
        <div><dt>{t("teamScoreboard.servicesTitle")}</dt><dd>{tasks === null ? "—" : String(tasks.length).padStart(2, "0")}</dd></div>
        <div><dt>{t("teamScoreboard.round")}</dt><dd>{round > 0 ? String(round).padStart(2, "0") : "—"}</dd></div>
      </dl>
    </PageHero>
  );
}
