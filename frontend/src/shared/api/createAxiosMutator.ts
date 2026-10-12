import type {
  AxiosError,
  AxiosInstance,
  AxiosRequestConfig,
} from "axios";

export function createAxiosMutator(api: AxiosInstance, http: AxiosInstance) {
  return <T>(
    config: AxiosRequestConfig,
    options?: AxiosRequestConfig,
  ): Promise<T> => {
    const requestUrl = options?.url ?? config.url ?? "";
    const isApiRequest = requestUrl.startsWith("/api/");
    const instance = isApiRequest ? api : http;
    const url = isApiRequest ? requestUrl.slice("/api".length) : requestUrl;

    return instance
      .request<T>({ ...config, ...options, url })
      .then(({ data }) => data);
  };
}

export type ErrorType<Error> = AxiosError<Error>;
export type BodyType<Body> = Body;
