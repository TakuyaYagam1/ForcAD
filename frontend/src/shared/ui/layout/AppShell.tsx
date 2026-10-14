import { useEffect, useState, type ReactNode } from "react";
import { Link, NavLink, useLocation } from "react-router-dom";
import { Sparkles } from "lucide-react";
import { useTranslation } from "react-i18next";
import { useScoreboardStore } from "@/entities/scoreboard/model/store";
import { BrandIcon } from "@/shared/ui/brand/BrandIcon";
import SideRays from "@/components/SideRays";
import { usePageEntrance } from "@/shared/lib/usePageEntrance";
import { gameStatusLabel } from "@/shared/lib/gameStatus";
import { brand } from "@/app/config/brand";
import { LanguageSwitch } from "./LanguageSwitch";

export function AppShell({ children, headerActions }: { children: ReactNode; headerActions?: ReactNode }) {
  const { t } = useTranslation("shell");
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
        {t("skip")}
      </a>

      <header className="site-header">
        <div className="header-inner">
          <Link
            to="/"
            className="brand-lockup"
            aria-label={t("home", { brand: brand.name })}
          >
            <BrandLogo />
            <span>{brand.name}</span>
          </Link>

          <nav className="main-nav" aria-label={t("navigation")}>
            <NavLink to="/" end>
              <BrandIcon name="podium" plain />
              {t("scoreboard")}
            </NavLink>
            <NavLink to="/live">
              <BrandIcon name="activity" plain />
              {t("events")}
            </NavLink>
            <NavLink to="/teams">
              <BrandIcon name="team" plain />
              {t("teams")}
            </NavLink>
          </nav>

          <div className="header-tools">
            <div className="header-status">
              {teams && !error && round > 0 && (
                <span className="header-round">{t("round", { round })}</span>
              )}
              <span
                className={`connection-dot ${error || (!connected && teams) ? "connection-dot--error" : teams ? "connection-dot--ready" : ""}`}
              />
              <span>
                {error || (!connected && teams)
                  ? t("disconnected")
                  : teams
                    ? gameStatusLabel(phase)
                    : t("connecting")}
              </span>
            </div>
            {headerActions}
            <LanguageSwitch />
          </div>
        </div>
      </header>

      <main ref={mainRef} id="main-content" className="site-main">
        {phase === "paused" && (
          <div
            className="mx-auto my-3 max-w-screen-xl rounded border border-amber-300/20 bg-amber-300/5 px-4 py-3 text-sm text-amber-100"
            role="status"
          >
            {t("paused")}
          </div>
        )}
        {phase === "finished" && (
          <div className="notice mx-auto my-3 max-w-screen-xl" role="status">
            {t("finished")}{" "}
            {resultsPending
              ? t("pending")
              : t("stopped")}
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
            title={t("animationsHint")}
          >
            <Sparkles size={14} />
            {t("animations", { state: motion ? t("on") : t("off") })}
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
