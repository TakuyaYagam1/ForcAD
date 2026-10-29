import type { ScoreboardTeam } from "@/entities/scoreboard/model/store";
import { useScoreboardStore } from "@/entities/scoreboard/model/store";
import { useViewportAnimations } from "@/shared/lib/useViewportAnimations";
import { ScoreValue } from "@/shared/ui/brand/ScoreValue";
import { TeamAvatar } from "@/shared/ui/brand/TeamAvatar";
import { AppShell } from "@/shared/ui/layout/AppShell";
import { ArrowUpRight } from "lucide-react";
import { useTranslation } from "react-i18next";
import { getLocale } from "@/shared/i18n";
import { useRef } from "react";
import { Link } from "react-router-dom";
import "./TeamsPage.css";

function TeamCard({ team, rank }: { team: ScoreboardTeam; rank: number }) {
  const { t } = useTranslation("public");
  const podium = rank >= 1 && rank <= 3;
  const locale = getLocale();
  const formattedRank = new Intl.NumberFormat(locale).format(rank);

  return (
    <Link
      to={`/team/${team.id}`}
      className={`team-card ${podium ? "team-card--podium" : ""}`}
      data-rank={rank}
    >
      {podium ? (
        <>
          <div className="team-podium-head">
            <TeamAvatar name={team.name} src={team.logo_path} />
          </div>
          <div className="team-podium-copy">
            <h2>{team.name}</h2>
            <span className="team-ip">{team.ip}</span>
            <span className="team-podium-place">
              {t("teams.podiumPlace", { rank: formattedRank })}
            </span>
          </div>
        </>
      ) : (
        <>
          <span className="team-card-place">
            {t("teams.podiumPlace", { rank: formattedRank })}
          </span>
          <div className="team-card-top">
            <TeamAvatar name={team.name} src={team.logo_path} />
            <div>
              <h2>{team.name}</h2>
              <span className="team-ip">{team.ip}</span>
            </div>
          </div>
        </>
      )}

      <div className="team-card-bottom">
        <div>
          <small>{t("teams.score")}</small>
          <ScoreValue value={team.score ?? 0} />
        </div>
        <span className="text-link">
          {t("teams.profile")} <ArrowUpRight size={16} />
        </span>
      </div>
    </Link>
  );
}

export function TeamsPage() {
  const { t } = useTranslation("public");
  const leadersRef = useRef<HTMLElement>(null);
  const teams = useScoreboardStore((state) => state.teams) ?? [];
  const ranked = teams.map((team, index) => ({ team, rank: index + 1 }));
  const leaders = ranked.slice(0, 3);
  const others = ranked.slice(3);
  useViewportAnimations(leadersRef, leaders.length > 0);

  return (
    <AppShell>
      <div className="teams-page">
        <h1 className="sr-only">{t("teams.leaders")}</h1>
        {ranked.length ? (
          <>
            {leaders.length > 0 && (
              <section
                ref={leadersRef}
                data-viewport-animation="running"
                className="teams-leaders"
                aria-label={t("teams.leaders")}
              >
                <div className="teams-podium">
                  {leaders.map(({ team, rank }) => (
                    <TeamCard key={team.id} team={team} rank={rank} />
                  ))}
                </div>
              </section>
            )}

            {others.length > 0 && (
              <section
                className="teams-roster"
                aria-label={t("teams.rosterLabel")}
              >
                {leaders.length > 0 && (
                  <div className="section-heading">
                    <h2>{t("teams.otherTeams")}</h2>
                  </div>
                )}
                <div className="teams-grid">
                  {others.map(({ team, rank }) => (
                    <TeamCard key={team.id} team={team} rank={rank} />
                  ))}
                </div>
              </section>
            )}
          </>
        ) : (
          <div className="empty-state mt-5">
            <h2>{t("teams.waitingTitle")}</h2>
            <p>{t("teams.waitingDescription")}</p>
          </div>
        )}
      </div>
    </AppShell>
  );
}
