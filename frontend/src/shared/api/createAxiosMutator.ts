import { AxiosHeaders } from "axios";
import type {
  AxiosError,
  AxiosInstance,
  AxiosRequestConfig,
  RawAxiosHeaders,
} from "axios";

export function createAxiosMutator(api: AxiosInstance, http: AxiosInstance) {
  let csrfToken: string | undefined;
  let pendingToken: Promise<string> | undefined;
  let sessionVersion = 0;
  const forgetToken = () => {
    sessionVersion++;
    csrfToken = undefined;
    pendingToken = undefined;
  };
  const rememberToken = (data: unknown): string | undefined => {
    const value = data && typeof data === "object" && "csrf_token" in data
      ? data.csrf_token : undefined;
    return typeof value === "string" && /^[0-9a-f]{64}$/.test(value) ? value : undefined;
  };
  const sessionToken = async (): Promise<string> => {
    if (csrfToken) return csrfToken;
    if (pendingToken) return pendingToken;
    const version = sessionVersion;
    const request = api.request({ url: "/admin/status/", method: "GET", timeout: 10000 })
      .then(({ data }) => {
        if (version !== sessionVersion) throw new Error("Administrator session changed; try again");
        csrfToken = rememberToken(data);
        if (!csrfToken) throw new Error("Administrator session has no CSRF token; sign in again");
        return csrfToken;
      }).finally(() => { if (pendingToken === request) pendingToken = undefined; });
    pendingToken = request;
    return request;
  };

  return async <T>(
    config: AxiosRequestConfig,
    options?: AxiosRequestConfig,
  ): Promise<T> => {
    const merged = { ...config, ...options };
    const requestUrl = merged.url ?? "";
    const isApiRequest = requestUrl.startsWith("/api/");
    const instance = isApiRequest ? api : http;
    const url = isApiRequest ? requestUrl.slice("/api".length) : requestUrl;
    const normalized = new URL(requestUrl, "https://forcad.invalid");
    const admin = requestUrl.startsWith("/api/admin/") &&
      normalized.origin === "https://forcad.invalid" &&
      normalized.pathname.startsWith("/api/admin/") && !requestUrl.includes("\\");
    const login = admin && normalized.pathname === "/api/admin/login/";
    const status = admin && normalized.pathname === "/api/admin/status/";
    const logout = admin && normalized.pathname === "/api/admin/logout/";
    const mutation = !["GET", "HEAD", "OPTIONS"].includes((merged.method ?? "GET").toUpperCase());
    const headers = new AxiosHeaders(merged.headers as RawAxiosHeaders | AxiosHeaders | undefined);
    const suppliedCSRF = headers.has("X-CSRF-Token");
    headers.delete("X-CSRF-Token");
    if (admin && merged.baseURL && merged.baseURL !== api.defaults?.baseURL) {
      throw new Error("Administrative requests must use the configured same-origin API");
    }
    if (admin && mutation && !login) {
      headers.set("X-CSRF-Token", await sessionToken());
    }
    const request = { ...merged, url };
    if (suppliedCSRF || (admin && mutation && !login)) request.headers = headers;
    if (login) forgetToken();
    const version = sessionVersion;
    try {
      const { data } = await instance.request<T>(request);
      if (version === sessionVersion) {
        if (login || status) csrfToken = rememberToken(data);
        if (logout) forgetToken();
      }
      return data;
    } catch (error) {
      if (admin && version === sessionVersion && [401, 403].includes((error as AxiosError).response?.status ?? 0)) forgetToken();
      // Never retry a mutation automatically: its side effects may have committed.
      throw error;
    }
  };
}

export type ErrorType<Error> = AxiosError<Error>;
export type BodyType<Body> = Body;
