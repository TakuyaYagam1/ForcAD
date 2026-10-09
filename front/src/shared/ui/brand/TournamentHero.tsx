import { useRef, type ReactNode } from "react";
import { GearMechanism } from "./GearMechanism";
import { ClockworkDial } from "./ClockworkDial";
import { useViewportAnimations } from "@/shared/lib/useViewportAnimations";
import "./TournamentHero.css";

export function HeroArt() {
  return (
    <div className="hero-art" aria-hidden="true">
      <div className="hero-orbit" />
      <ClockworkDial />
      <img className="hero-engraving" src="/brand/mechanism.webp" alt="" />
      <GearMechanism />
      <img className="hero-trophy" src="/brand/trophy.webp" alt="" />
    </div>
  );
}

export function TournamentHero({
  title,
  eyebrow,
  subtitle,
  children,
  compact = false,
}: {
  title: string;
  eyebrow?: string;
  subtitle?: string;
  children?: ReactNode;
  compact?: boolean;
}) {
  const heroRef = useRef<HTMLElement>(null);
  useViewportAnimations(heroRef);

  return (
    <section
      ref={heroRef}
      data-viewport-animation="running"
      className={`tournament-hero ${compact ? "tournament-hero--compact" : ""}`}
    >
      <div className="hero-copy">
        {eyebrow && <p className="eyebrow">{eyebrow}</p>}
        <h1>{title}</h1>
        {subtitle && <p className="hero-subtitle">{subtitle}</p>}
        {children}
      </div>

      <HeroArt />
    </section>
  );
}
