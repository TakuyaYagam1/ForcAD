import assert from "node:assert/strict";
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { loadEnv } from "vite";
import {
  createBrandConfig,
  DEFAULT_BRAND,
  hslTriplet,
  rgbTriplet,
} from "../src/app/config/brand.ts";

test("blank brand values use the neutral ForcAD defaults", () => {
  assert.deepEqual(
    createBrandConfig({
      VITE_BRAND_NAME: "   ",
      VITE_BRAND_LOGO: "",
      VITE_BRAND_PRIMARY: "",
      VITE_BRAND_ACCENT: "   ",
    }),
    DEFAULT_BRAND,
  );
});

test("valid build-time brand values are trimmed and normalized", () => {
  assert.deepEqual(
    createBrandConfig({
      VITE_BRAND_NAME: "  Scoreboard  ",
      VITE_BRAND_LOGO: " /brand/custom-mark.svg ",
      VITE_BRAND_PRIMARY: "#3879d8",
      VITE_BRAND_ACCENT: "#28A8A1",
    }),
    {
      name: "Scoreboard",
      logo: "/brand/custom-mark.svg",
      primary: "#3879D8",
      accent: "#28A8A1",
    },
  );
});

test("brand colors produce valid HSL tokens for Tailwind CSS variables", () => {
  assert.equal(hslTriplet(DEFAULT_BRAND.primary), "220 82% 65%");
  assert.equal(hslTriplet(DEFAULT_BRAND.accent), "177 53% 47%");
  assert.equal(rgbTriplet(DEFAULT_BRAND.primary), "91 141 239");
  assert.equal(rgbTriplet(DEFAULT_BRAND.accent), "56 183 176");
});

test("invalid colors and logo paths fall back to safe defaults", () => {
  assert.deepEqual(
    createBrandConfig({
      VITE_BRAND_LOGO: "//other.example/logo.svg",
      VITE_BRAND_PRIMARY: "url(javascript:alert(1))",
      VITE_BRAND_ACCENT: "rgb(0 0 0)",
    }),
    DEFAULT_BRAND,
  );
});

test("quoted example colors survive Vite dotenv parsing", () => {
  const fixtureDirectory = mkdtempSync(join(tmpdir(), "forcad-brand-env-"));
  const example = readFileSync(new URL("../.env.example", import.meta.url), "utf8");
  writeFileSync(join(fixtureDirectory, ".env.test"), example);

  const keys = ["VITE_BRAND_PRIMARY", "VITE_BRAND_ACCENT"] as const;
  const previousValues = keys.map((key) => process.env[key]);

  try {
    for (const key of keys) delete process.env[key];
    const values = loadEnv("test", fixtureDirectory, "VITE_BRAND_");
    assert.equal(values.VITE_BRAND_PRIMARY, "#5B8DEF");
    assert.equal(values.VITE_BRAND_ACCENT, "#38B7B0");
  } finally {
    keys.forEach((key, index) => {
      if (previousValues[index] === undefined) delete process.env[key];
      else process.env[key] = previousValues[index];
    });
    rmSync(fixtureDirectory, { recursive: true, force: true });
  }
});
