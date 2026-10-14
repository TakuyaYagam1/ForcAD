import { useTranslation } from "react-i18next";
import { getLanguage, setLanguage } from "@/shared/i18n";
import { LANGUAGES } from "@/shared/i18n/locale";
import "./LanguageSwitch.css";

export function LanguageSwitch() {
  const { t } = useTranslation("shell");
  const selected = getLanguage();

  return (
    <div className="language-switch" role="group" aria-label={t("language")}>
      {LANGUAGES.map((language) => (
        <button
          key={language}
          type="button"
          lang={language}
          aria-label={language === "ru" ? "Русский" : "English"}
          aria-pressed={selected === language}
          onClick={() => void setLanguage(language)}
        >
          {language.toUpperCase()}
        </button>
      ))}
    </div>
  );
}
