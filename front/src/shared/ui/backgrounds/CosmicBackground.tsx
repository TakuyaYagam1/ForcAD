import type { ReactNode } from "react";
import Galaxy from "@/components/Galaxy";
import { cn } from "@/lib/utils";

interface CosmicBackgroundProps {
  children: ReactNode;
  className?: string;
  /** Плотность звёзд (из Galaxy) */
  density?: number;
  /** Скорость анимации звёзд (из Galaxy) */
  starSpeed?: number;
}

export function CosmicBackground({
  children,
  className,
}: CosmicBackgroundProps) {
  return (
    <div
      className={cn(
        "relative min-h-screen overflow-hidden bg-gradient-to-b from-[#050816] via-[#020617] to-black",
        className
      )}
    >
      <div className="pointer-events-none absolute inset-0 z-1 opacity-80">
        <Galaxy
          density={0.5}
          mouseRepulsion={false}
          mouseInteraction={false}
          glowIntensity={0.3}
          saturation={0.5}
          hueShift={140}
          twinkleIntensity={0}
          rotationSpeed={0.05}
          repulsionStrength={1}
          autoCenterRepulsion={0}
          starSpeed={0.1}
          speed={0.5}
        />
      </div>

      <div className="pointer-events-none absolute inset-0 z-10 bg-[radial-gradient(circle_at_top,_rgba(15,23,42,0.9)_0,_transparent_55%),radial-gradient(circle_at_bottom,_rgba(15,23,42,0.95)_0,_transparent_60%)] mix-blend-soft-light" />

      <div className="relative z-10">{children}</div>
    </div>
  );
}
