import { useEffect, useRef } from "react";
import { Mesh, Program, Renderer, Triangle } from "ogl";
import "./LightRays.css";

// Adapted from React Bits Light Rays by David Haz.
// https://reactbits.dev/backgrounds/light-rays
// Source license: https://github.com/DavidHDev/react-bits/blob/main/LICENSE.md

type RaysOrigin =
  | "top-left"
  | "top-center"
  | "top-right"
  | "bottom-left"
  | "bottom-center"
  | "bottom-right";

interface LightRaysProps {
  enabled?: boolean;
  raysOrigin?: RaysOrigin;
  raysColor?: string;
  raysSpeed?: number;
  lightSpread?: number;
  rayLength?: number;
  fadeDistance?: number;
  distortion?: number;
  shimmerIntensity?: number;
}

const vertex = `
attribute vec2 position;
void main() {
  gl_Position = vec4(position, 0.0, 1.0);
}`;

const fragment = `
precision highp float;
uniform float iTime;
uniform vec2 iResolution;
uniform vec2 rayPos;
uniform vec2 rayDir;
uniform vec3 raysColor;
uniform float raysSpeed;
uniform float lightSpread;
uniform float rayLength;
uniform float fadeDistance;
uniform float distortion;
uniform float shimmerIntensity;

float rayStrength(vec2 coord, float seedA, float seedB, float speed) {
  vec2 delta = coord - rayPos;
  float distance = max(length(delta), 0.0001);
  float angle = dot(delta / distance, rayDir);
  angle += distortion * sin(iTime * 2.0 + distance * 0.01) * 0.2;
  float spread = pow(max(angle, 0.0), 1.0 / max(lightSpread, 0.001));
  float reach = max(iResolution.x * rayLength, 1.0);
  float lengthFalloff = clamp((reach - distance) / reach, 0.0, 1.0);
  float fade = max(iResolution.x * fadeDistance, 1.0);
  float fadeFalloff = clamp((fade - distance) / fade, 0.5, 1.0);
  float strength = clamp(
    (0.45 + 0.15 * sin(angle * seedA + iTime * speed)) +
    (0.30 + 0.20 * cos(-angle * seedB + iTime * speed)), 0.0, 1.0
  );
  return strength * lengthFalloff * fadeFalloff * spread;
}

void main() {
  vec2 coord = vec2(gl_FragCoord.x, iResolution.y - gl_FragCoord.y);
  vec2 delta = coord - rayPos;
  float distance = max(length(delta), 0.0001);
  float angle = atan(delta.y, delta.x);
  float time = iTime * raysSpeed;
  float drift = sin(time * 0.65) * 0.12;
  float cone = pow(max(dot(delta / distance, rayDir), 0.0),
                   1.0 / max(lightSpread, 0.001));
  float reach = max(iResolution.x * rayLength, 1.0);
  float falloff = pow(clamp(1.0 - distance / reach, 0.0, 1.0), 1.4);

  float wide = pow(0.5 + 0.5 * sin((angle + drift) * 12.0 - time * 1.6), 1.0);
  float fine = pow(0.5 + 0.5 * sin(angle * 27.0 + time * 2.1 + distance * 0.002), 2.0);
  float sweep = pow(0.5 + 0.5 * sin(angle * 7.0 - time * 1.1), 5.0);
  float base = rayStrength(coord, 36.2214, 21.11349, 1.5 * raysSpeed) * 0.5;
  base += rayStrength(coord, 22.3991, 18.0234, 1.1 * raysSpeed) * 0.4;
  float glow = exp(-distance / max(min(iResolution.x, iResolution.y) * 0.38, 1.0));
  float energy = base * 0.18 + glow * 0.12;
  energy += cone * falloff * shimmerIntensity * (wide * 0.5 + fine * 0.42 + sweep * 0.32);

  float colorWave = 0.5 + 0.5 * sin(time * 1.35 + angle * 5.0 - distance * 0.0015);
  vec3 gold = mix(raysColor, vec3(1.0, 0.93, 0.70), 0.2 + colorWave * 0.8);
  gl_FragColor = vec4(gold, clamp(energy, 0.0, 0.82));
}`;

export default function LightRays({
  enabled = true,
  raysOrigin = "bottom-right",
  raysColor = "#ffbd63",
  raysSpeed = 0.85,
  lightSpread = 1.1,
  rayLength = 2.8,
  fadeDistance = 1.75,
  distortion = 0.12,
  shimmerIntensity = 0.9,
}: LightRaysProps) {
  const containerRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;

    let renderer: Renderer;
    try {
      renderer = new Renderer({
        alpha: true,
        premultipliedAlpha: false,
        antialias: false,
        depth: false,
        powerPreference: "low-power",
        dpr: 1,
      });
    } catch {
      return; // The CSS glow remains visible if WebGL is unavailable.
    }

    const { gl } = renderer;
    const color = /^#?([a-f\d]{2})([a-f\d]{2})([a-f\d]{2})$/i.exec(raysColor);
    const uniforms = {
      iTime: { value: 0 },
      iResolution: { value: [1, 1] },
      rayPos: { value: [0, 0] },
      rayDir: { value: [0, 1] },
      raysColor: {
        value: color
          ? color.slice(1).map((part) => parseInt(part, 16) / 255)
          : [1, 0.77, 0.45],
      },
      raysSpeed: { value: raysSpeed },
      lightSpread: { value: lightSpread },
      rayLength: { value: rayLength },
      fadeDistance: { value: fadeDistance },
      distortion: { value: distortion },
      shimmerIntensity: { value: shimmerIntensity },
    };

    const geometry = new Triangle(gl);
    const program = new Program(gl, { vertex, fragment, uniforms });
    const mesh = new Mesh(gl, { geometry, program });
    const reducedMotion = window.matchMedia("(prefers-reduced-motion: reduce)");
    const start = performance.now();

    let frame = 0;
    let lastPaint = 0;
    let disposed = false;

    container.appendChild(gl.canvas);

    const canAnimate = () =>
      enabled && !reducedMotion.matches && !document.hidden && !disposed;

    const draw = (now: number) => {
      uniforms.iTime.value = canAnimate() ? (now - start) * 0.001 : 0;
      renderer.render({ scene: mesh });
    };

    const resize = () => {
      const width = container.clientWidth;
      const height = container.clientHeight;
      if (!width || !height || disposed) return;

      renderer.dpr = Math.min(
        window.devicePixelRatio || 1,
        width < 600 ? 1 : 1.25,
      );
      renderer.setSize(width, height);

      const w = gl.canvas.width;
      const h = gl.canvas.height;
      const bottom = raysOrigin.startsWith("bottom");
      const left = raysOrigin.endsWith("left");
      const right = raysOrigin.endsWith("right");
      const x = left ? 0 : right ? w : w / 2;

      uniforms.iResolution.value = [w, h];
      uniforms.rayPos.value = [x, bottom ? h * 1.04 : -h * 0.04];
      uniforms.rayDir.value = [
        left ? 0.6 : right ? -0.6 : 0,
        bottom ? -0.8 : 0.8,
      ];

      draw(performance.now());
    };

    const tick = (now: number) => {
      if (!canAnimate()) return;

      if (now - lastPaint >= 1000 / 30) {
        draw(now);
        lastPaint = now;
      }

      frame = requestAnimationFrame(tick);
    };

    const syncMotion = () => {
      cancelAnimationFrame(frame);
      draw(performance.now());
      if (canAnimate()) frame = requestAnimationFrame(tick);
    };

    const observer = new ResizeObserver(resize);
    observer.observe(container);

    window.addEventListener("resize", resize);
    document.addEventListener("visibilitychange", syncMotion);
    reducedMotion.addEventListener("change", syncMotion);

    resize();
    syncMotion();

    return () => {
      disposed = true;
      cancelAnimationFrame(frame);
      observer.disconnect();

      window.removeEventListener("resize", resize);
      document.removeEventListener("visibilitychange", syncMotion);
      reducedMotion.removeEventListener("change", syncMotion);

      geometry.remove();
      program.remove();
      gl.canvas.remove();
      gl.getExtension("WEBGL_lose_context")?.loseContext();
    };
  }, [
    enabled,
    raysOrigin,
    raysColor,
    raysSpeed,
    lightSpread,
    rayLength,
    fadeDistance,
    distortion,
    shimmerIntensity,
  ]);

  return (
    <div
      ref={containerRef}
      className="light-rays-container"
      aria-hidden="true"
    />
  );
}
