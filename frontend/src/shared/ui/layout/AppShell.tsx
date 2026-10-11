import { useEffect, useState, type ReactNode } from "react";
import { Link, NavLink, useLocation } from "react-router-dom";
import { Sparkles } from "lucide-react";
import { useScoreboardStore } from "@/entities/scoreboard/model/store";
import { BrandIcon } from "@/shared/ui/brand/BrandIcon";
import SideRays from "@/components/SideRays";
import { usePageEntrance } from "@/shared/lib/usePageEntrance";
import { gameStatusLabel } from "@/shared/lib/gameStatus";
import { brand } from "@/app/config/brand";

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
          rayColor1={brand.primary}
          rayColor2={brand.accent}
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
            aria-label={`${brand.name} - рейтинг команд`}
          >
            <BrandLogo />
            <span>{brand.name}</span>
          </Link>

          <nav className="main-nav" aria-label="Основная навигация">
            <NavLink to="/" end>
              <BrandIcon name="podium" plain />
              Рейтинг
            </NavLink>
            <NavLink to="/live">
              <BrandIcon name="activity" plain />
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
          <BrandLogo />
          <span>
            {brand.name}<span className="footer-divider">/</span>Attack-Defense
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

function BrandLogo() {
  const [imageFailed, setImageFailed] = useState(false);

  if (imageFailed) {
    return (
      <svg
        className="brand-mark-fallback"
        viewBox="0 0 48 48"
        fill="none"
        aria-hidden="true"
        focusable="false"
      >
        <rect
          x="4"
          y="4"
          width="40"
          height="40"
          rx="11"
          stroke="var(--brand-primary)"
          strokeOpacity=".42"
        />
        <path
          d="M17 34V14h16M17 23h12"
          stroke="var(--brand-primary)"
          strokeWidth="3.5"
          strokeLinecap="round"
          strokeLinejoin="round"
        />
        <path
          d="M31 29v5"
          stroke="var(--brand-accent)"
          strokeWidth="3.5"
          strokeLinecap="round"
        />
      </svg>
    );
  }

  return <img src={brand.logo} alt="" onError={() => setImageFailed(true)} />;
}
