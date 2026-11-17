import { createContext, useContext } from "react";

export const AdminNotificationContext = createContext<((message: string) => void) | null>(null);

export function useAdminNotification(): (message: string) => void {
  const notify = useContext(AdminNotificationContext);
  if (!notify) {
    throw new Error("useAdminNotification must be used within AdminNotificationProvider");
  }
  return notify;
}
