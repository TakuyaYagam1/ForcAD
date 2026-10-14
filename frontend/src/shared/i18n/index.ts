import i18next from "i18next";
import { initReactI18next } from "react-i18next";
import { adminRu, adminEn } from "./admin.ts";
import { commonRu, commonEn } from "./common.ts";
import { publicRu, publicEn } from "./public.ts";
import { shellRu, shellEn } from "./shell.ts";
import {
  LANGUAGES,
  LANGUAGE_STORAGE_KEY,
  readLanguage,
  resolveLanguage,
  type Language,
} from "./locale.ts";

export const resources = {
  ru: { common: commonRu, public: publicRu, admin: adminRu, shell: shellRu },
  en: { common: commonEn, public: publicEn, admin: adminEn, shell: shellEn },
};

declare module "i18next" {
  interface CustomTypeOptions {
    defaultNS: "common";
    resources: typeof resources.ru;
    enableSelector: false;
    strictKeyChecks: true;
    returnNull: false;
  }
}

export const i18n = i18next;

function saveLanguage(language: string) {
  const supported = resolveLanguage(language);
  if (typeof document !== "undefined") document.documentElement.lang = supported;
  try {
    localStorage.setItem(LANGUAGE_STORAGE_KEY, supported);
  } catch {
    // Language switching also works when browser storage is unavailable.
  }
}

i18n.on("languageChanged", saveLanguage);

export const i18nReady = i18n.use(initReactI18next).init({
  resources,
  lng: readLanguage(),
  supportedLngs: [...LANGUAGES],
  fallbackLng: "ru",
  load: "languageOnly",
  defaultNS: "common",
  initAsync: false,
  interpolation: { escapeValue: false },
  react: { useSuspense: false },
});

export function getLanguage(): Language {
  return resolveLanguage(i18n.resolvedLanguage ?? i18n.language);
}

export function getLocale(): string {
  return getLanguage() === "en" ? "en-US" : "ru-RU";
}

export function setLanguage(language: Language) {
  return i18n.changeLanguage(resolveLanguage(language));
}
