import type { ScoreboardTeam } from "@/entities/scoreboard/model/store";
import { useScoreboardStore } from "@/entities/scoreboard/model/store";
import { ScoreValue } from "@/shared/ui/brand/ScoreValue";
import { TeamAvatar } from "@/shared/ui/brand/TeamAvatar";
import { ArrowUpRight, Search } from "lucide-react";
import { useTranslation } from "react-i18next";
import { useState } from "react";
import { Link } from "react-router-dom";
import "./TeamsPage.css";

function TeamCard({ team, rank }: {
  team: ScoreboardTeam;
  rank: number
}) {
  const { t } = useTranslation("public");
  return (
    <Link to={`/team/${team.id}`} className="team-catalogue-card" data-rank={rank}>
      <div className="team-catalogue-art">
        <span className="team-catalogue-rank">{String(rank).padStart(2, "0")}</span>
        <TeamAvatar name={team.name} src={team.logo_path} size="large" />
        <ArrowUpRight size={18} strokeWidth={1} aria-hidden="true" />
      </div>
      <div className="team-catalogue-caption"><div><h2>{team.name}</h2><span className="team-ip">{team.ip}</span></div><ScoreValue value={team.score ?? 0} /></div>
      <span className="team-catalogue-profile">{t("teams.profile")} <ArrowUpRight size={13} strokeWidth={1} aria-hidden="true" /></span>
    </Link>
  );
}

export function TeamsPage() {
  const { t, i18n } = useTranslation("public");
  const teams = useScoreboardStore((state) => {
    return state.teams
  }) ?? [];
  const npcTeams = useScoreboardStore((state) => {
    return state.npcTeams
  });
  const [filter, setFilter] = useState("");
  const query = filter.trim().toLocaleLowerCase(i18n.language);
  const ranked = teams.map((team, index) => {
    return {
      team,
      rank: index + 1
    }
  }).filter(({ team }) => {
    return `${team.name} ${team.ip}`.toLocaleLowerCase(i18n.language).includes(query)
  });
  return (
    <>
      <div className="teams-page">
        <h1 className="sr-only">{t("teams.title")}</h1>
        {npcTeams.length > 0 && (
          <section className="teams-test-target" aria-label={t("teams.testTarget")}>
            <div className="section-heading">
              <h2>{t("teams.testTarget")}</h2>
              <p>{t("teams.testTargetDescription")}</p>
            </div>
            <div className="teams-catalogue">
              {npcTeams.map((team) => {
                return (
                  <article className="team-catalogue-card" key={team.id}>
                    <div className="team-catalogue-art">
                      <TeamAvatar name={team.name} src={team.logo_path} size="large" />
                    </div>
                    <div className="team-catalogue-caption">
                      <div><h2>{team.name}</h2><span className="team-ip">{team.ip}</span></div>
                    </div>
                  </article>
                )
              })}
            </div>
          </section>
        )}
        <div className="teams-toolbar"><span>{t("teams.currentRankingOrder")}</span><label className="search-field"><Search size={17} aria-hidden="true" /><input type="search" aria-label={t("teams.searchLabel")} placeholder={t("teams.searchPlaceholder")} value={filter} onChange={(event) => {
          return setFilter(event.target.value)
        }} /></label></div>
        {ranked.length ? (
          <section className="teams-catalogue" aria-label={t("teams.profilesAndResults")}>
            {ranked.map(({ team, rank }) => {
              return <TeamCard key={team.id} team={team} rank={rank} />
            })}
          </section>
        ) : (
          <div className="empty-state"><h2>{t(query ? "teams.notFoundTitle" : "teams.waitingTitle")}</h2><p>{t(query ? "teams.notFoundDescription" : "teams.waitingDescription")}</p></div>
        )}
      </div>
    </>
  );
}
