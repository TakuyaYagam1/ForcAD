import { useRef, type ReactNode } from "react";
import { useViewportAnimations } from "@/shared/lib/useViewportAnimations";
import "./PageHero.css";

export function PageHero({
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
      className={`page-hero ${compact ? "page-hero--compact" : ""}`}
    >
      <div className="hero-copy">
        {eyebrow && <p className="eyebrow">{eyebrow}</p>}
        <h1>{title}</h1>
        {subtitle && <p className="hero-subtitle">{subtitle}</p>}
        {children}
      </div>

    </section>
  );
}
