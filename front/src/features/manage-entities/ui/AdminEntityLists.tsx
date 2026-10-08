import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { fetchTeamsAdmin } from "@/entities/team/api/admin";
import { fetchTasksAdmin } from "@/entities/task/api/admin";
import { apiErrorMessage } from "@/shared/lib/apiError";

export function AdminEntityLists() {
  const teams = useQuery({
    queryKey: ["admin-teams"],
    queryFn: fetchTeamsAdmin,
    refetchInterval: 10000,
  });
  const tasks = useQuery({
    queryKey: ["admin-tasks"],
    queryFn: fetchTasksAdmin,
    refetchInterval: 10000,
  });
  return (
    <div className="grid gap-4 md:grid-cols-2 my-6">
      {[
        { title: "Все команды", kind: "team", query: teams },
        { title: "Все сервисы", kind: "task", query: tasks },
      ].map(({ title, kind, query }) => (
        <section className="admin-form p-5" key={kind}>
          <h2>{title}</h2>
          <p className="text-xs text-slate-400 mb-3">
            Отключённые записи можно открыть и включить снова.
          </p>
          {query.isPending && <p>Загружаем…</p>}
          {query.error && (
            <div role="alert">
              {apiErrorMessage(query.error, "Не удалось загрузить список")}{" "}
              <button type="button" onClick={() => void query.refetch()}>
                Повторить
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
                  {item.active ? "Активна" : "Отключена"}
                </span>
              </li>
            ))}
          </ul>
          {query.data?.length === 0 && <p>Записей пока нет.</p>}
        </section>
      ))}
    </div>
  );
}
