import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { fetchTeamsAdmin } from "@/entities/team/api/admin";
import { fetchTasksAdmin } from "@/entities/task/api/admin";
import { apiErrorMessage } from "@/shared/lib/apiError";
import { useTranslation } from "react-i18next";

export function AdminEntityLists() {
  const { t } = useTranslation("admin");
  const teams = useQuery({
    queryKey: ["admin-teams"],
    queryFn: ({ signal }) => fetchTeamsAdmin(signal),
    refetchInterval: 10000,
  });
  const tasks = useQuery({
    queryKey: ["admin-tasks"],
    queryFn: ({ signal }) => fetchTasksAdmin(signal),
    refetchInterval: 10000,
  });
  const sections = [
    { title: t("entities.teamsTitle"), kind: "team", query: teams },
    { title: t("entities.servicesTitle"), kind: "task", query: tasks },
  ] as const;
  return (
    <div className="grid gap-4 md:grid-cols-2 my-6">
      {sections.map(({ title, kind, query }) => (
        <section className="admin-form p-5" key={kind}>
          <h2>{title}</h2>
          <p className="text-xs text-slate-400 mb-3">
            {t("entities.disabledDescription")}
          </p>
          {query.isPending && <p>{t("common.loading")}</p>}
          {query.error && (
            <div role="alert">
              {apiErrorMessage(query.error, t("entities.loadError"))}{" "}
              <button type="button" onClick={() => void query.refetch()}>
                {t("common.retry")}
              </button>
            </div>
          )}
          <ul className="space-y-2 max-h-64 overflow-auto">
            {query.data?.map((item) => (
              <li key={item.id} className="flex justify-between gap-3">
                <Link
                  to={`/admin/${kind}/${item.id}`}
                  className="hover:underline"
                >
                  {item.name}
                </Link>
                <span
                  className={
                    item.active ? "text-emerald-300" : "text-slate-400"
                  }
                >
                  {item.active ? t("common.active") : t("common.inactive")}
                </span>
              </li>
            ))}
          </ul>
          {query.data?.length === 0 && <p>{t("entities.empty")}</p>}
        </section>
      ))}
    </div>
  );
}
