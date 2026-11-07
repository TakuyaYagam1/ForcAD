// src/features/auth-admin/model/useAdminLoginMutation.ts
import { useMutation } from "@tanstack/react-query";
import { useAdminAuthStore, type AdminLoginPayload } from "./useAdminAuth";

export function useAdminLoginMutation() {
  const login = useAdminAuthStore((s) => {
    return s.login
  });

  return useMutation({
    mutationKey: ["adminLogin"],
    mutationFn: (payload: AdminLoginPayload) => {
      return login(payload)
    },
  });
}
