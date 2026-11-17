import { Button } from "@/components/ui/button";
import {
  AdminEntityLists,
  type AdminEntityKind,
} from "@/features/manage-entities/ui/AdminEntityLists";
import { AdminShell } from "@/widgets/admin-shell/ui/AdminShell";
import { Plus } from "lucide-react";
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";

interface AdminEntitiesPageProps {
  kind: AdminEntityKind;
}

export function AdminEntitiesPage({ kind }: AdminEntitiesPageProps) {
  const { t } = useTranslation("admin");
  const isTeams = kind === "teams";

  return (
    <AdminShell>
      <div className="admin-page">
        <div className="admin-heading">
          <div>
            <p className="eyebrow">{t("common.organizerPanel")}</p>
            <h1>
              {isTeams
                ? t("entities.teamsTitle")
                : t("entities.servicesTitle")}
            </h1>
          </div>
          <Button asChild variant="outline">
            <Link to={isTeams ? "/admin/team/create" : "/admin/task/create"}>
              <Plus aria-hidden="true" />
              {isTeams
                ? t("scoreboard.addTeam")
                : t("scoreboard.addService")}
            </Link>
          </Button>
        </div>
        <AdminEntityLists key={kind} kind={kind} />
      </div>
    </AdminShell>
  );
}
