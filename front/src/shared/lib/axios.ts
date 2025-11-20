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
