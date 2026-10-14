import type { ScoreboardTeam } from "@/entities/scoreboard/model/store";
import { useScoreboardStore } from "@/entities/scoreboard/model/store";
import { useViewportAnimations } from "@/shared/lib/useViewportAnimations";
import { BrandIcon } from "@/shared/ui/brand/BrandIcon";
import { PageHero } from "@/shared/ui/brand/PageHero";
import { RankBadge } from "@/shared/ui/brand/RankBadge";
import { ScoreValue } from "@/shared/ui/brand/ScoreValue";
import { TeamAvatar } from "@/shared/ui/brand/TeamAvatar";
import { AppShell } from "@/shared/ui/layout/AppShell";
import { ArrowUpRight, Search } from "lucide-react";
import { useTranslation } from "react-i18next";
import { getLocale } from "@/shared/i18n";
import { useRef, useState } from "react";
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
            <RankBadge rank={rank} />
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
          <span className="team-card-place">#{formattedRank}</span>
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
  const [filter, setFilter] = useState("");
  const locale = getLocale();
  const query = filter.trim().toLocaleLowerCase(locale);

  const filtered = teams
    .map((team, index) => ({ team, rank: index + 1 }))
    .filter(({ team }) =>
      `${team.name} ${team.ip}`.toLocaleLowerCase(locale).includes(query),
    );

  const leaders = filtered.filter(({ rank }) => rank <= 3);
  const others = filtered.filter(({ rank }) => rank > 3);
  useViewportAnimations(leadersRef, leaders.length > 0);

  return (
    <AppShell>
      <div className="teams-page">
        <PageHero
          compact
          title={t("teams.title")}
          subtitle={t("teams.subtitle")}
        >
          <div className="hero-metadata">
            <span>
              <BrandIcon name="team" />
              {teams.length
                ? t("teams.participantCount", {
                    count: teams.length,
                    formattedCount: new Intl.NumberFormat(locale).format(
                      teams.length,
                    ),
                  })
                : t("teams.participantCountEmpty")}
            </span>
            <span>{t("teams.profilesAndResults")}</span>
          </div>
        </PageHero>

        <div className="events-toolbar">
          <label className="search-field">
            <Search size={17} />
            <input
              type="search"
              aria-label={t("teams.searchLabel")}
              placeholder={t("teams.searchPlaceholder")}
              value={filter}
              onChange={(event) => setFilter(event.target.value)}
            />
          </label>
          <p className="events-caption">{t("teams.currentRankingOrder")}</p>
        </div>

        {filtered.length ? (
          <>
            {leaders.length > 0 && (
              <section
                ref={leadersRef}
                data-viewport-animation="running"
                className="teams-leaders"
                aria-labelledby="teams-leaders-title"
              >
                <div className="section-heading leaders-heading">
                  <h2 id="teams-leaders-title">
                    <BrandIcon name="podium" plain />
                    {t("teams.leaders")}
                  </h2>
                  <span className="leaders-caption">{t("teams.topThree")}</span>
                </div>
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
                    <h2>
                      <BrandIcon name="team" plain />
                      {t("teams.otherTeams")}
                    </h2>
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
            <BrandIcon name="team" />
            <h2>
              {query ? t("teams.notFoundTitle") : t("teams.waitingTitle")}
            </h2>
            <p>
              {query
                ? t("teams.notFoundDescription")
                : t("teams.waitingDescription")}
            </p>
          </div>
        )}
      </div>
    </AppShell>
  );
}
