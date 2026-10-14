import { i18n } from "../../shared/i18n/index.ts";

export interface BrandConfig {
  name: string;
  logo: string;
  primary: string;
  accent: string;
}

export interface BrandEnvironment {
  VITE_BRAND_NAME?: string;
  VITE_BRAND_LOGO?: string;
  VITE_BRAND_PRIMARY?: string;
  VITE_BRAND_ACCENT?: string;
}

export const DEFAULT_BRAND: BrandConfig = {
  name: "ForcAD",
  logo: "/brand/forcad-mark.svg",
  primary: "#5B8DEF",
  accent: "#38B7B0",
};

const HEX_COLOR = /^#[0-9a-f]{6}$/i;
const LOCAL_LOGO_PATH = /^\/(?!\/)[a-z0-9._/-]+$/i;

function valueOrDefault(value: string | undefined, fallback: string): string {
  const trimmed = value?.trim();
  return trimmed || fallback;
}

function colorOrDefault(value: string | undefined, fallback: string): string {
  const trimmed = value?.trim();
  return trimmed && HEX_COLOR.test(trimmed) ? trimmed.toUpperCase() : fallback;
}

function logoOrDefault(value: string | undefined): string {
  const trimmed = value?.trim();
  if (!trimmed || !LOCAL_LOGO_PATH.test(trimmed)) return DEFAULT_BRAND.logo;
  if (trimmed.split("/").some((part) => part === "." || part === "..")) {
    return DEFAULT_BRAND.logo;
  }
  return trimmed;
}

export function createBrandConfig(environment: BrandEnvironment): BrandConfig {
  return {
    name: valueOrDefault(environment.VITE_BRAND_NAME, DEFAULT_BRAND.name),
    logo: logoOrDefault(environment.VITE_BRAND_LOGO),
    primary: colorOrDefault(
      environment.VITE_BRAND_PRIMARY,
      DEFAULT_BRAND.primary,
    ),
    accent: colorOrDefault(
      environment.VITE_BRAND_ACCENT,
      DEFAULT_BRAND.accent,
    ),
  };
}

function toRgb(color: string): [number, number, number] {
  return [1, 3, 5].map((offset) => Number.parseInt(color.slice(offset, offset + 2), 16)) as [
    number,
    number,
    number,
  ];
}

export function rgbTriplet(color: string): string {
  return toRgb(color).join(" ");
}

function toHsl(color: string): [number, number, number] {
  const [red, green, blue] = toRgb(color).map((channel) => channel / 255);
  const max = Math.max(red, green, blue);
  const min = Math.min(red, green, blue);
  const delta = max - min;
  const lightness = (max + min) / 2;
  let hue = 0;
  let saturation = 0;

  if (delta !== 0) {
    saturation = delta / (1 - Math.abs(2 * lightness - 1));
    if (max === red) hue = ((green - blue) / delta) % 6;
    else if (max === green) hue = (blue - red) / delta + 2;
    else hue = (red - green) / delta + 4;
    hue *= 60;
    if (hue < 0) hue += 360;
  }

  return [Math.round(hue), Math.round(saturation * 100), Math.round(lightness * 100)];
}

export function hslTriplet(color: string): string {
  const [hue, saturation, lightness] = toHsl(color);
  return `${hue} ${saturation}% ${lightness}%`;
}

export const brand = createBrandConfig(
  (import.meta as ImportMeta & { env?: BrandEnvironment }).env ?? {},
);

export function applyBrandConfig(): void {
  const root = document.documentElement;
  const primaryRgb = rgbTriplet(brand.primary);
  const accentRgb = rgbTriplet(brand.accent);
  const [accentHue, accentSaturation] = toHsl(brand.accent);
  const [, , primaryLightness] = toHsl(brand.primary);

  root.style.setProperty("--brand-primary", brand.primary);
  root.style.setProperty("--brand-primary-rgb", primaryRgb);
  root.style.setProperty("--brand-accent", brand.accent);
  root.style.setProperty("--brand-accent-rgb", accentRgb);
  root.style.setProperty("--primary", hslTriplet(brand.primary));
  root.style.setProperty("--ring", hslTriplet(brand.primary));
  root.style.setProperty(
    "--primary-foreground",
    primaryLightness > 62 ? "216 43% 6%" : "0 0% 100%",
  );
  root.style.setProperty(
    "--accent",
    `${accentHue} ${Math.min(accentSaturation, 42)}% 17%`,
  );
  root.style.setProperty(
    "--accent-foreground",
    `${accentHue} ${accentSaturation}% 72%`,
  );

  applyBrandMetadata();
}

export function applyBrandMetadata(): void {
  document.title = i18n.t("shell:home", { brand: brand.name });
  document
    .querySelector<HTMLMetaElement>('meta[name="description"]')
    ?.setAttribute(
      "content",
      i18n.t("shell:description", { brand: brand.name }),
    );
}
