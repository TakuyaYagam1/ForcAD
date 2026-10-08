import { useState } from "react";
import { SERVER_URL } from "@/app/config";

function resolveImageSource(src?: string) {
  if (!src) return undefined;
  try {
    return new URL(src, `${SERVER_URL}/`).href;
  } catch {
    return src;
  }
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
  const [failedSrc, setFailedSrc] = useState<string | null>(null);
  // Relative team logos are hosted by the same ForcAD server as the API.
  const imageSrc = resolveImageSource(src);
  const showImage = Boolean(imageSrc && failedSrc !== imageSrc);
  return (
    <span className={`team-avatar team-avatar--${size}`}>
      {showImage ? (
        <img
          src={imageSrc}
          alt={`Логотип ${name}`}
          loading="lazy"
          onError={() => setFailedSrc(imageSrc ?? null)}
        />
      ) : (
        <span aria-hidden="true">{name.slice(0, 2).toUpperCase()}</span>
      )}
    </span>
  );
}
