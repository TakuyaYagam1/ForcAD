// src/features/auth-admin/model/useAdminAuth.ts
import { create } from "zustand";
import { devtools } from "zustand/middleware";
import { api } from "@/shared/lib/axios";

export interface AdminLoginPayload {
  username: string;
  password: string;
}

export interface AdminUser {
  username: string;
  // сюда можно добавить роли, id и т.п.
}

interface AuthState {
  isAuthChecked: boolean; // проверяли ли сессию на бэке
  isAuthenticated: boolean;
  user: AdminUser | null;
  error: string | null;

  login: (payload: AdminLoginPayload) => Promise<void>;
  logout: () => Promise<void>;
  checkSession: () => Promise<void>;
}

export const useAdminAuthStore = create<AuthState>()(
  devtools((set) => ({
    isAuthChecked: false,
    isAuthenticated: false,
    user: null,
    error: null,

    async login(payload) {
      try {
        set({ error: null });
        await api.post("/admin/login/", payload);
        // если бэк умеет возвращать юзера — можно его прочитать:
        // const { data } = await http.get<AdminUser>("/admin/me/");
        set({
          isAuthenticated: true,
          isAuthChecked: true,
          user: { username: payload.username },
          error: null,
        });
      } catch (e: any) {
        console.error(e);
        set({
          isAuthenticated: false,
          isAuthChecked: true,
          user: null,
          error: "Неверный логин или пароль",
        });
        throw e;
      }
    },

    async logout() {
      try {
        await api.post("/admin/logout/"); // подстрой под свой бэк
      } catch (e) {
        console.error(e);
      } finally {
        set({
          isAuthenticated: false,
          user: null,
          error: null,
        });
      }
    },

    async checkSession() {
      try {
        // подстрой под реальный эндпоинт, если есть:
        // const { data } = await http.get<AdminUser>("/admin/me/");
        // set({ isAuthenticated: true, isAuthChecked: true, user: data, error: null });

        // если такого эндпоинта нет, можно просто считать, что кука = сессия,
        // и оставить isAuthenticated=false по умолчанию, а checkSession сделать no-op.
        set({
          isAuthenticated: false,
          isAuthChecked: true,
          user: null,
          error: null,
        });
      } catch (e) {
        console.error(e);
        set({
          isAuthenticated: false,
          isAuthChecked: true,
          user: null,
          error: null,
        });
      }
    },
  }))
);
