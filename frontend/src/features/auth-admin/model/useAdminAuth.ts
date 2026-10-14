import { create } from "zustand";
import {
  adminLogin,
  adminLogout,
  getAdminSessionStatus,
} from "@/shared/api/generated/endpoints";
import { isAxiosError } from "axios";

export interface AdminLoginPayload {
  username: string;
  password: string;
}
export interface AdminUser {
  username: string;
}

export type AdminAuthError =
  | { kind: "invalidCredentials" }
  | { kind: "loginFailed"; cause: unknown }
  | { kind: "logoutFailed"; cause: unknown }
  | { kind: "sessionCheckFailed"; cause: unknown };

interface AuthState {
  isAuthChecked: boolean;
  isAuthenticated: boolean;
  user: AdminUser | null;
  error: AdminAuthError | null;
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
      const data = await adminLogin(payload);
      if (revision === current)
        set({
          isAuthenticated: true,
          isAuthChecked: true,
          user: { username: data.username },
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
              ? { kind: "invalidCredentials" }
              : { kind: "loginFailed", cause: error },
        });
      throw error;
    }
  },
  async logout() {
    const current = ++revision;
    set({ error: null });
    try {
      await adminLogout();
      if (revision === current)
        set({
          isAuthenticated: false,
          isAuthChecked: true,
          user: null,
          error: null,
        });
    } catch (error) {
      if (revision === current)
        set({ error: { kind: "logoutFailed", cause: error } });
      throw error;
    }
  },
  checkSession() {
    if (checkInFlight) return checkInFlight;
    const current = revision;
    checkInFlight = (async () => {
      try {
        const data = await getAdminSessionStatus();
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
          error: invalid ? null : { kind: "sessionCheckFailed", cause: error },
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
