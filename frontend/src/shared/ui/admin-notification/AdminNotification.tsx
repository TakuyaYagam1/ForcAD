import {
  useCallback,
  useState,
  type ReactNode,
} from "react";
import { X } from "lucide-react";
import { useTranslation } from "react-i18next";
import { AdminNotificationContext } from "./useAdminNotification";

interface AdminNotificationProviderProps {
  children: ReactNode;
}

export function AdminNotificationProvider({
  children,
}: AdminNotificationProviderProps) {
  const { t } = useTranslation("common");
  const [message, setMessage] = useState<string | null>(null);
  const notify = useCallback((nextMessage: string) => {
    setMessage(nextMessage);
  }, []);

  return (
    <AdminNotificationContext.Provider value={notify}>
      {children}
      {message && (
        <div
          className="fixed right-4 top-24 z-[100] flex max-w-sm items-center gap-3 rounded-md border border-emerald-400/40 bg-slate-950 px-4 py-3 text-sm text-slate-100 shadow-lg max-[900px]:top-36"
          role="status"
          aria-live="polite"
          data-testid="admin-notification"
        >
          <span className="min-w-0 flex-1">{message}</span>
          <button
            type="button"
            className="rounded-sm p-1 text-slate-300 transition-colors hover:bg-white/10 hover:text-white focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-emerald-300"
            aria-label={t("dialog.close")}
            onClick={() => {
              setMessage(null);
            }}
          >
            <X className="size-4" aria-hidden="true" />
          </button>
        </div>
      )}
    </AdminNotificationContext.Provider>
  );
}

