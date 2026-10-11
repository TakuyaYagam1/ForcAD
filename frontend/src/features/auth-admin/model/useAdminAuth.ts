import { create } from "zustand";
import { api } from "@/shared/lib/axios";
import { apiErrorMessage } from "@/shared/lib/apiError";
import { isAxiosError } from "axios";
export interface AdminLoginPayload {
  username: string;
  password: string;
}
export interface AdminUser {
  username: string;
}
interface AuthState {
  isAuthChecked: boolean;
  isAuthenticated: boolean;
  user: AdminUser | null;
  error: string | null;
  login(payload: AdminLoginPayload): Promise<void>;
  logout(): Promise<void>;
  checkSession(): Promise<void>;
}
let checkInFlight: Promise<void> | null = null;
let revision = 0;
export const useAdminAuthStore = create<AuthState>((set) => ({
  isAuthChecked: false,
  isAuthenticated: false,
  user: null,
  error: null,
  async login(payload) {
    const current = ++revision;
    set({ error: null });
    try {
      const { data } = await api.post<{ username?: string }>(
        "/admin/login/",
        payload,
      );
      if (revision === current)
        set({
          isAuthenticated: true,
          isAuthChecked: true,
          user: { username: data.username ?? payload.username },
          error: null,
        });
    } catch (error) {
      if (revision === current)
        set({
          isAuthenticated: false,
          isAuthChecked: true,
          user: null,
          error:
            isAxiosError(error) && error.response?.status === 403
              ? "Неверный логин или пароль"
              : apiErrorMessage(error, "Не удалось войти"),
        });
      throw error;
    }
  },
  async logout() {
    const current = ++revision;
    set({ error: null });
    try {
      await api.post("/admin/logout/");
      if (revision === current)
        set({
          isAuthenticated: false,
          isAuthChecked: true,
          user: null,
          error: null,
        });
    } catch (error) {
      if (revision === current)
        set({
          error: apiErrorMessage(
            error,
            "Не удалось завершить сессию. Повтори выход.",
          ),
        });
      throw error;
    }
  },
  checkSession() {
    if (checkInFlight) return checkInFlight;
    const current = revision;
    checkInFlight = (async () => {
      try {
        const { data } = await api.get<{ username: string }>("/admin/status/");
        if (revision === current)
          set({
            isAuthenticated: true,
            isAuthChecked: true,
            user: { username: data.username },
            error: null,
          });
      } catch (error) {
        if (revision !== current) return;
        const invalid =
          isAxiosError(error) &&
          [401, 403].includes(error.response?.status ?? 0);
        set({
          isAuthenticated: false,
          isAuthChecked: true,
          user: null,
          error: invalid
            ? null
            : apiErrorMessage(error, "Не удалось проверить сессию"),
        });
      }
    })().finally(() => {
      checkInFlight = null;
    });
    return checkInFlight;
  },
}));
window.addEventListener("forcad:admin-session-expired", () => {
  ++revision;
  useAdminAuthStore.setState({
    isAuthChecked: true,
    isAuthenticated: false,
    user: null,
    error: null,
  });
});
