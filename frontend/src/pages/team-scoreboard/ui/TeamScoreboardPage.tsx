import { useParams } from "react-router-dom";
import { TeamScoreboardWidget } from "@/widgets/team-scoreboard/ui/TeamScoreboardWidget";
import { normalizeUuid } from "@/shared/lib/uuid";
import { useTranslation } from "react-i18next";

export function TeamScoreboardPage() {
  const { t } = useTranslation("public");
  const { teamId: rawTeamId } = useParams();
  const isInvalid = !normalizeUuid(rawTeamId);

  return (
    <>
      {isInvalid ? (
        <div className="notice" role="alert">
          {t("teamScoreboard.invalidIdWithValue", {
            id: rawTeamId ?? ""
          })}
        </div>
      ) : (
        <TeamScoreboardWidget />
      )}
    </>
  );
}
