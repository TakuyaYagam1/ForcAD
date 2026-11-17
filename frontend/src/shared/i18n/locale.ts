export const LANGUAGES = ["ru", "en"] as const;
export type Language = (typeof LANGUAGES)[number];

export const LANGUAGE_STORAGE_KEY = "forcad-language";

export function resolveLanguage(value: unknown): Language {
  return value === "en" ? "en" : "ru";
}

export function readLanguage(): Language {
  try {
    return resolveLanguage(localStorage.getItem(LANGUAGE_STORAGE_KEY));
  } catch {
    return "ru";
  }
}
