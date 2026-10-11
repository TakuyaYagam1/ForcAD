type RequestScheme = "http" | "https";

function parseHttpUrl(value: string): URL | undefined {
  try {
    const url = new URL(value);
    if (url.protocol !== "http:" && url.protocol !== "https:") {
      return undefined;
    }
    return url;
  } catch {
    return undefined;
  }
}

function parseOrigin(value: string): URL | undefined {
  const url = parseHttpUrl(value);
  if (
    !url ||
    url.username ||
    url.password ||
    url.pathname !== "/" ||
    url.search ||
    url.hash
  ) {
    return undefined;
  }
  return url;
}

export function getSameOriginProxyOrigin(
  requestOrigin: string | undefined,
  requestHost: string | undefined,
  requestScheme: RequestScheme,
  target: string,
): string | undefined {
  if (!requestOrigin || !requestHost) return undefined;

  const incoming = parseOrigin(requestOrigin);
  const host = parseOrigin(`${requestScheme}://${requestHost}`);
  const backend = parseHttpUrl(target);
  if (!incoming || !host || !backend || incoming.origin !== host.origin) {
    return undefined;
  }

  return backend.origin;
}
