import { useState } from "react";
import { Search, WifiOff } from "lucide-react";
import { useLiveScoreboardStore } from "@/entities/live-scoreboard/model/store";
import { BrandIcon } from "@/shared/ui/brand/BrandIcon";
import { TournamentHero } from "@/shared/ui/brand/TournamentHero";
import { EventItem } from "@/shared/ui/brand/RecentEvents";

export function LiveScoreboardFrame() {
  const events = useLiveScoreboardStore((state) => state.events);
  const error = useLiveScoreboardStore((state) => state.error);
  const [filter, setFilter] = useState("");
  const query = filter.trim().toLocaleLowerCase("ru-RU");
  const filtered = events.filter((event) =>
    `${event.attackerName} ${event.victimName} ${event.taskName}`
      .toLocaleLowerCase("ru-RU")
      .includes(query),
  );
  return (
    <div className="w-full">
      <TournamentHero
        compact
        title="События"
        subtitle="Каждый захват флага меняет расстановку сил."
      >
        <div className="hero-metadata">
          <span>
            <BrandIcon name="flag" />
            Захваты флагов
          </span>
          <span>Последние события соревнования</span>
        </div>
      </TournamentHero>
      {error && (
        <div className="notice" role="status">
          <WifiOff size={16} />
          Ожидаем восстановления соединения с лентой событий.
        </div>
      )}
      <div className="events-toolbar">
        <label className="search-field">
          <Search size={17} />
          <input
            type="search"
            aria-label="Найти событие по команде или сервису"
            value={filter}
            onChange={(event) => setFilter(event.target.value)}
            placeholder="Команда или сервис…"
          />
        </label>
        <p className="events-caption">
          Событий в этой сессии: {events.length} · новые сверху
        </p>
      </div>
      {filtered.length ? (
        <ul className="events-list">
          {filtered.map((event) => (
            <EventItem key={event.id} event={event} />
          ))}
        </ul>
      ) : (
        <div className="empty-state mt-5">
          <BrandIcon name="flag" />
          <h2>{query ? "События не найдены" : "Пока тихо"}</h2>
          <p>
            {query
              ? "Попробуй другое название команды или сервиса."
              : "Здесь появятся захваты флагов: атакующая команда, соперник, сервис и полученные очки."}
          </p>
        </div>
      )}
      <p className="table-hint">
        Лента хранит до 100 последних событий, полученных во время открытой
        сессии.
      </p>
    </div>
  );
}
