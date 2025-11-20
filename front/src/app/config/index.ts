// src/app/config/index.ts

let url = "";

if (import.meta.env.DEV) {
  // как в старом Vue: фронт отдельно, бэк на 8080
  url = "http://127.0.0.1:8080";
} else {
  // в проде фронт и бэк на одном домене
  url = window.location.origin;
}

export const SERVER_URL = url;
export const API_URL = `${SERVER_URL}/api`;
