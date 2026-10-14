import { useState } from "react";
import { UsersRound } from "lucide-react";
import { SERVER_URL } from "@/app/config";
import { useTranslation } from "react-i18next";

function resolveImageSource(src?: string) {
  if (!src) return undefined;
  try {
    return new URL(src, `${SERVER_URL}/`).href;
  } catch {
    return src;
  }
}

function getInitials(name: string): string {
  return name
    .trim()
    .split(/\s+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((part) => Array.from(part)[0] ?? "")
    .join("")
    .toLocaleUpperCase();
}

export function TeamAvatar({
  name,
  src,
  size = "normal",
}: {
  name: string;
  src?: string;
  size?: "small" | "normal" | "large";
}) {
  const { t } = useTranslation("common");
  const [failedSrc, setFailedSrc] = useState<string | null>(null);
  // Relative team logos are hosted by the same ForcAD server as the API.
  const imageSrc = resolveImageSource(src);
  const showImage = Boolean(imageSrc && failedSrc !== imageSrc);
  const initials = getInitials(name);
  return (
    <span className={`team-avatar team-avatar--${size}`}>
      {showImage ? (
        <img
          src={imageSrc}
          alt={t("team.logoAlt", { name })}
          loading="lazy"
          onError={() => setFailedSrc(imageSrc ?? null)}
        />
      ) : (
        <span aria-hidden="true">
          {initials || <UsersRound size={17} strokeWidth={1.8} />}
        </span>
      )}
    </span>
  );
}
