import { useState } from "react";
import { Link } from "react-router-dom";
import { Search, ArrowUpRight } from "lucide-react";
import { AppShell } from "@/shared/ui/layout/AppShell";
import { useScoreboardStore } from "@/entities/scoreboard/model/store";
import { TournamentHero } from "@/shared/ui/brand/TournamentHero";
import { TeamAvatar } from "@/shared/ui/brand/TeamAvatar";
import { BrandIcon } from "@/shared/ui/brand/BrandIcon";
import { ScoreValue } from "@/shared/ui/brand/ScoreValue";
import { RankBadge } from "@/shared/ui/brand/RankBadge";
import type { Team } from "@/entities/team/model/types";
import "./TeamsPage.css";

function TeamCard({ team, rank }: { team: Team; rank: number }) {
  const medal = rank >= 1 && rank <= 3;

  return (
    <Link
      to={`/team/${team.id}`}
      className={`team-card ${medal ? "team-card--medallion" : ""}`}
      data-rank={rank}
    >
      {medal ? (
        <>
          <div className="team-medallion">
            <RankBadge rank={rank} />
            <TeamAvatar name={team.name} src={team.logo_path} />
            <span className="team-medallion-place">{rank} место</span>
          </div>
          <div className="medallion-copy">
            <h2>{team.name}</h2>
            <span className="team-ip">{team.ip}</span>
          </div>
        </>
      ) : (
        <>
          <span className="team-card-place">#{rank}</span>
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
          <small>Очки</small>
          <ScoreValue value={team.score ?? 0} />
        </div>
        <span className="text-link">
          Профиль <ArrowUpRight size={16} />
        </span>
      </div>
    </Link>
  );
}

export function TeamsPage() {
  const teams = useScoreboardStore((state) => state.teams) ?? [];
  const [filter, setFilter] = useState("");
  const query = filter.trim().toLocaleLowerCase("ru-RU");

  const filtered = teams
    .map((team, index) => ({ team, rank: index + 1 }))
    .filter(({ team }) =>
      `${team.name} ${team.ip}`.toLocaleLowerCase("ru-RU").includes(query),
    );

  const leaders = filtered.filter(({ rank }) => rank <= 3);
  const others = filtered.filter(({ rank }) => rank > 3);

  return (
    <AppShell>
      <div className="teams-page">
        <TournamentHero
          compact
          title="Команды"
          subtitle="За каждым результатом — своя команда."
        >
          <div className="hero-metadata">
            <span>
              <BrandIcon name="team" />
              Участников: {teams.length || "—"}
            </span>
            <span>Профили и результаты</span>
          </div>
        </TournamentHero>

        <div className="events-toolbar">
          <label className="search-field">
            <Search size={17} />
            <input
              type="search"
              aria-label="Поиск команды"
              placeholder="Найти команду или IP…"
              value={filter}
              onChange={(event) => setFilter(event.target.value)}
            />
          </label>
          <p className="events-caption">В порядке текущего рейтинга</p>
        </div>

        {filtered.length ? (
          <>
            {leaders.length > 0 && (
              <section
                className="teams-leaders"
                aria-labelledby="teams-leaders-title"
              >
                <div className="section-heading leaders-heading">
                  <h2 id="teams-leaders-title">
                    <BrandIcon name="podium" plain />
                    Лидеры рейтинга
                  </h2>
                  <span className="leaders-caption">Топ-3</span>
                </div>
                <div className="teams-podium">
                  {leaders.map(({ team, rank }) => (
                    <TeamCard key={team.id} team={team} rank={rank} />
                  ))}
                </div>
              </section>
            )}

            {others.length > 0 && (
              <section className="teams-roster" aria-label="Остальные команды">
                {leaders.length > 0 && (
                  <div className="section-heading">
                    <h2>
                      <BrandIcon name="team" plain />
                      Остальные команды
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
            <h2>{query ? "Команда не найдена" : "Ожидаем участников"}</h2>
            <p>
              {query
                ? "Попробуй другое название или IP-адрес."
                : "Команды появятся после подключения к серверу соревнования."}
            </p>
          </div>
        )}
      </div>
    </AppShell>
  );
}
