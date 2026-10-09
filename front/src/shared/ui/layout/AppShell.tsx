import { useEffect, useState, type ReactNode } from "react";
import { Link, NavLink, useLocation } from "react-router-dom";
import { Sparkles } from "lucide-react";
import { useScoreboardStore } from "@/entities/scoreboard/model/store";
import { BrandIcon } from "@/shared/ui/brand/BrandIcon";
import SideRays from "@/components/SideRays";
import { usePageEntrance } from "@/shared/lib/usePageEntrance";
import { gameStatusLabel } from "@/shared/lib/gameStatus";

export function AppShell({ children }: { children: ReactNode }) {
  const location = useLocation();
  const mainRef = usePageEntrance(location.pathname, location.key);

  const round = useScoreboardStore(
    (state) => state.runtimeRound ?? state.round,
  );
  const teams = useScoreboardStore((state) => state.teams);
  const error = useScoreboardStore((state) => state.error);
  const connected = useScoreboardStore((state) => state.connected);
  const phase = useScoreboardStore((state) => state.phase);
  const resultsPending = useScoreboardStore((state) => state.resultsPending);

  const [motion, setMotion] = useState(() => {
    try {
      return localStorage.getItem("cf-motion") !== "off";
    } catch {
      return true;
    }
  });

  useEffect(() => {
    document.documentElement.dataset.motion = motion ? "on" : "off";

    try {
      localStorage.setItem("cf-motion", motion ? "on" : "off");
    } catch {
      /* Storage may be unavailable in private browsers. */
    }
  }, [motion]);

  return (
    <div className="brand-app">
      <div className="brand-background" aria-hidden="true">
        <SideRays
          enabled={motion}
          origin="bottom-right"
          rayColor1="#ffbd63"
          rayColor2="#fff1c9"
          speed={2.5}
          intensity={3}
          spread={2.2}
          tilt={-10}
          saturation={1.05}
          blend={0.4}
          falloff={1.35}
          opacity={0.35}
        />
      </div>

      <a href="#main-content" className="skip-link">
        К содержимому
      </a>

      <header className="site-header">
        <div className="header-inner">
          <Link
            to="/"
            className="brand-lockup"
            aria-label="Кубок Федерации 2026 — рейтинг"
          >
            <img src="/brand/mark.webp" alt="" />
            <span>
              Кубок Федерации <b>2026</b>
            </span>
          </Link>

          <nav className="main-nav" aria-label="Основная навигация">
            <NavLink to="/" end>
              <BrandIcon name="podium" plain />
              Рейтинг
            </NavLink>
            <NavLink to="/live">
              <BrandIcon name="flag" plain />
              События
            </NavLink>
            <NavLink to="/teams">
              <BrandIcon name="team" plain />
              Команды
            </NavLink>
          </nav>

          <div className="header-status">
            {teams && !error && round > 0 && (
              <span className="header-round">Раунд {round}</span>
            )}
            <span
              className={`connection-dot ${error || (!connected && teams) ? "connection-dot--error" : teams ? "connection-dot--ready" : ""}`}
            />
            <span>
              {error || (!connected && teams)
                ? "Нет соединения"
                : teams
                  ? gameStatusLabel(phase)
                  : "Подключение"}
            </span>
          </div>
        </div>
      </header>

      <main ref={mainRef} id="main-content" className="site-main">
        {phase === "paused" && (
          <div
            className="mx-auto my-3 max-w-screen-xl rounded border border-amber-300/20 bg-amber-300/5 px-4 py-3 text-sm text-amber-100"
            role="status"
          >
            Игра приостановлена: новые флаги не принимаются и проверки не запускаются.
            Уже запущенные проверки могут завершиться и обновить результаты.
          </div>
        )}
        {phase === "finished" && (
          <div className="notice mx-auto my-3 max-w-screen-xl" role="status">
            Игра завершилась. Прием флагов закрыт.
            {resultsPending
              ? " Ожидаем результаты последних проверок перед публикацией итогов."
              : " Новые раунды и проверки не запускаются."}
          </div>
        )}
        {children}
      </main>

      <footer className="site-footer">
        <div>
          <img src="/brand/mark.webp" alt="" />
          <span>
            Кубок Федерации 2026<span className="footer-divider">/</span>
            Attack–Defense
          </span>
        </div>

        <div className="footer-actions">
          <button
            type="button"
            onClick={() => setMotion(!motion)}
            aria-pressed={motion}
            title="Включить или отключить декоративные анимации"
          >
            <Sparkles size={14} />
            Анимации {motion ? "вкл." : "выкл."}
          </button>
        </div>
      </footer>
    </div>
  );
}
