import { useEffect, useRef } from "react";
import { Mesh, Program, Renderer, Triangle } from "ogl";
import "./SideRays.css";

// Adapted from React Bits Side Rays by David Haz.
// https://reactbits.dev/c/backgrounds/side-rays
// Source license: https://github.com/DavidHDev/react-bits/blob/main/LICENSE.md

type RaysOrigin = "top-right" | "top-left" | "bottom-right" | "bottom-left";

interface SideRaysProps {
  enabled?: boolean;
  speed?: number;
  rayColor1?: string;
  rayColor2?: string;
  intensity?: number;
  spread?: number;
  origin?: RaysOrigin;
  tilt?: number;
  saturation?: number;
  blend?: number;
  falloff?: number;
  opacity?: number;
  followMouse?: boolean;
  mouseInfluence?: number;
  className?: string;
}

const hexToRgb = (hex: string): [number, number, number] => {
  const match = /^#?([a-f\d]{2})([a-f\d]{2})([a-f\d]{2})$/i.exec(hex);

  return match
    ? [
        parseInt(match[1], 16) / 255,
        parseInt(match[2], 16) / 255,
        parseInt(match[3], 16) / 255,
      ]
    : [1, 1, 1];
};

const vertex = `
attribute vec2 position;

void main() {
  gl_Position = vec4(position, 0.0, 1.0);
}`;

const fragment = `
precision highp float;

uniform float iTime;
uniform vec2 iResolution;
uniform float iSpeed;
uniform vec3 iRayColor1;
uniform vec3 iRayColor2;
uniform float iIntensity;
uniform float iSpread;
uniform float iFlipX;
uniform float iFlipY;
uniform float iTilt;
uniform float iSaturation;
uniform float iBlend;
uniform float iFalloff;
uniform float iOpacity;
uniform vec2 iMouse;
uniform float iMouseInfluence;

float rayStrength(vec2 source, vec2 direction, vec2 coord,
                  float seedA, float seedB, float speed) {
  vec2 delta = coord - source;
  float distance = max(length(delta), 0.0001);
  float cosAngle = dot(delta / distance, direction);

  return clamp(
    (0.45 + 0.15 * sin(cosAngle * seedA + iTime * speed)) +
    (0.30 + 0.20 * cos(-cosAngle * seedB + iTime * speed)),
    0.0, 1.0
  ) * clamp((iResolution.x - distance) / iResolution.x, 0.5, 1.0);
}

void main() {
  vec2 fragCoord = gl_FragCoord.xy;

  if (iFlipX > 0.5) fragCoord.x = iResolution.x - fragCoord.x;
  if (iFlipY > 0.5) fragCoord.y = iResolution.y - fragCoord.y;

  vec2 coord = vec2(fragCoord.x, iResolution.y - fragCoord.y);
  vec2 rayPos = vec2(iResolution.x * 1.1, -0.5 * iResolution.y);

  float mouseTilt = (iMouse.x * 16.0 - iMouse.y * 6.0) * iMouseInfluence;
  float tiltRad = (iTilt + mouseTilt) * 3.14159265 / 180.0;

  float cs = cos(tiltRad);
  float sn = sin(tiltRad);
  vec2 rel = coord - rayPos;
  vec2 tiltedCoord = vec2(rel.x * cs - rel.y * sn,
                          rel.x * sn + rel.y * cs) + rayPos;

  float halfSpread = (iSpread + iMouse.y * iMouseInfluence * 0.15) * 0.275;

  vec2 direction1 = normalize(vec2(cos(0.785398 + halfSpread),
                                    sin(0.785398 + halfSpread)));
  vec2 direction2 = normalize(vec2(cos(0.785398 - halfSpread),
                                    sin(0.785398 - halfSpread)));

  vec4 rays1 = vec4(iRayColor1, 1.0) *
    rayStrength(rayPos, direction1, tiltedCoord, 36.2214, 21.11349, iSpeed);
  vec4 rays2 = vec4(iRayColor2, 1.0) *
    rayStrength(rayPos, direction2, tiltedCoord, 22.3991, 18.0234, iSpeed * 0.2);

  vec4 color = rays1 * (1.0 - iBlend) * 0.9 + rays2 * iBlend * 0.9;

  float distanceToLight = length(fragCoord -
    vec2(rayPos.x, iResolution.y - rayPos.y)) / iResolution.y;
  float brightness = iIntensity * 0.4 /
    pow(max(distanceToLight, 0.001), iFalloff);

  brightness *= 1.0 + iMouse.x * iMouseInfluence * 0.25;
  color.rgb *= brightness;

  float gray = dot(color.rgb, vec3(0.299, 0.587, 0.114));
  color.rgb = mix(vec3(gray), color.rgb, iSaturation);
  color.a = max(color.r, max(color.g, color.b)) * iOpacity;

  gl_FragColor = color;
}`;

export default function SideRays({
  enabled = true,
  speed = 2.5,
  rayColor1 = "#ffbd63",
  rayColor2 = "#fff1c9",
  intensity = 3,
  spread = 2.2,
  origin = "bottom-right",
  tilt = -10,
  saturation = 1.05,
  blend = 0.4,
  falloff = 1.35,
  opacity = 0.85,
  followMouse = true,
  mouseInfluence = 0.2,
  className = "",
}: SideRaysProps) {
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

    const uniforms = {
      iTime: { value: 0 },
      iResolution: { value: [1, 1] },
      iSpeed: { value: speed },
      iRayColor1: { value: hexToRgb(rayColor1) },
      iRayColor2: { value: hexToRgb(rayColor2) },
      iIntensity: { value: intensity },
      iSpread: { value: spread },
      iFlipX: { value: origin.endsWith("left") ? 1 : 0 },
      iFlipY: { value: origin.startsWith("bottom") ? 1 : 0 },
      iTilt: { value: tilt },
      iSaturation: { value: saturation },
      iBlend: { value: blend },
      iFalloff: { value: falloff },
      iOpacity: { value: opacity },
      iMouse: { value: [0, 0] },
      iMouseInfluence: { value: Math.max(0, Math.min(mouseInfluence, 1)) },
    };

    const geometry = new Triangle(gl);
    const program = new Program(gl, { vertex, fragment, uniforms });
    const mesh = new Mesh(gl, { geometry, program });

    const reducedMotion = window.matchMedia("(prefers-reduced-motion: reduce)");
    const finePointer = window.matchMedia("(hover: hover) and (pointer: fine)");

    const start = performance.now();
    const targetMouse = { x: 0, y: 0 };
    const currentMouse = { x: 0, y: 0 };

    let bounds = container.getBoundingClientRect();
    let frame = 0;
    let lastPaint = 0;
    let lastDraw = start;
    let disposed = false;

    container.appendChild(gl.canvas);

    const canAnimate = () =>
      enabled && !reducedMotion.matches && !document.hidden && !disposed;

    const draw = (now: number) => {
      const delta = Math.min(Math.max(now - lastDraw, 0), 64);
      lastDraw = now;

      const smoothing = 1 - Math.exp(-delta * 0.006);
      const interactive = canAnimate() && followMouse && finePointer.matches;

      currentMouse.x = interactive
        ? currentMouse.x + (targetMouse.x - currentMouse.x) * smoothing
        : 0;

      currentMouse.y = interactive
        ? currentMouse.y + (targetMouse.y - currentMouse.y) * smoothing
        : 0;

      uniforms.iMouse.value[0] = currentMouse.x;
      uniforms.iMouse.value[1] = currentMouse.y;
      uniforms.iTime.value = canAnimate() ? (now - start) * 0.001 : 0;

      renderer.render({ scene: mesh });
    };

    const resize = () => {
      const width = container.clientWidth;
      const height = container.clientHeight;

      if (!width || !height || disposed) return;

      bounds = container.getBoundingClientRect();

      // Soft background rays do not need display resolution. Keep the shader
      // buffer small even on large or high-density screens; CSS scales it up.
      renderer.dpr = Math.min(0.5, 960 / Math.max(width, height));

      renderer.setSize(width, height);
      uniforms.iResolution.value = [gl.canvas.width, gl.canvas.height];
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

    const resetMouse = () => {
      targetMouse.x = 0;
      targetMouse.y = 0;
    };

    const onPointerMove = (event: PointerEvent) => {
      if (
        event.pointerType !== "mouse" ||
        !followMouse ||
        !finePointer.matches ||
        !canAnimate() ||
        !bounds.width ||
        !bounds.height
      )
        return;

      targetMouse.x = Math.max(
        -1,
        Math.min(1, ((event.clientX - bounds.left) / bounds.width) * 2 - 1),
      );

      targetMouse.y = Math.max(
        -1,
        Math.min(1, ((event.clientY - bounds.top) / bounds.height) * 2 - 1),
      );
    };

    const observer = new ResizeObserver(resize);
    observer.observe(container);

    window.addEventListener("resize", resize);
    window.addEventListener("pointermove", onPointerMove, { passive: true });
    window.addEventListener("blur", resetMouse);
    document.addEventListener("pointerleave", resetMouse);
    document.addEventListener("visibilitychange", syncMotion);
    reducedMotion.addEventListener("change", syncMotion);
    finePointer.addEventListener("change", syncMotion);

    resize();
    syncMotion();

    return () => {
      disposed = true;
      cancelAnimationFrame(frame);
      observer.disconnect();

      window.removeEventListener("resize", resize);
      window.removeEventListener("pointermove", onPointerMove);
      window.removeEventListener("blur", resetMouse);
      document.removeEventListener("pointerleave", resetMouse);
      document.removeEventListener("visibilitychange", syncMotion);
      reducedMotion.removeEventListener("change", syncMotion);
      finePointer.removeEventListener("change", syncMotion);

      geometry.remove();
      program.remove();
      gl.canvas.remove();
      gl.getExtension("WEBGL_lose_context")?.loseContext();
    };
  }, [
    enabled,
    speed,
    rayColor1,
    rayColor2,
    intensity,
    spread,
    origin,
    tilt,
    saturation,
    blend,
    falloff,
    opacity,
    followMouse,
    mouseInfluence,
  ]);

  return (
    <div
      ref={containerRef}
      className={`side-rays-container ${className}`.trim()}
      aria-hidden="true"
    />
  );
}
