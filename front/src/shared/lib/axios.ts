// src/shared/lib/axios.ts
import axios from "axios";
import { SERVER_URL, API_URL } from "@/app/config";

export const http = axios.create({
  baseURL: SERVER_URL,
  withCredentials: true,
});

export const api = axios.create({
  baseURL: API_URL,
  withCredentials: true,
});

api.interceptors.response.use(
  (response) => response,
  (error) => {
    const path = String(error.config?.url ?? "");
    if (
      [401, 403].includes(error.response?.status) &&
      path.startsWith("/admin/") &&
      !["/admin/login/", "/admin/status/"].includes(path)
    ) {
      window.dispatchEvent(new Event("forcad:admin-session-expired"));
    }
    return Promise.reject(error);
  },
);
