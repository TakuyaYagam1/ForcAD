import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import type { Team } from "@/entities/team/model/types";
import { MAX_TEAM_NAME_LENGTH } from "@/shared/lib/adminValidation";
import { TeamAvatar } from "@/shared/ui/brand/TeamAvatar";
import type { FormEvent } from "react";
import { useTranslation } from "react-i18next";

type TeamFieldChange = <Field extends keyof Team>(
  field: Field,
  value: Team[Field],
) => void;

interface TeamFormProps {
  team: Team;
  isCreate: boolean;
  saving: boolean;
  canSave: boolean;
  onChange: TeamFieldChange;
  onSubmit: (event: FormEvent<HTMLFormElement>) => void;
  onCancel: () => void;
}

export function TeamForm({
  team,
  isCreate,
  saving,
  canSave,
  onChange,
  onSubmit,
  onCancel,
}: TeamFormProps) {
  const { t } = useTranslation("admin");
  const isNpc = team.kind === "npc";
  return (
    <form onSubmit={onSubmit} noValidate>
      <Card className="admin-form admin-editor-form !p-0">
        <CardHeader>
          <CardTitle className="text-sm admin-text-primary">
            {isCreate
              ? t("team.newTitle")
              : t("team.existingTitle", {
                name: team.name,
                id: team.id ?? "-"
              })}
          </CardTitle>
          <CardDescription className="text-xs admin-text-muted">
            {t("team.formDescription")}
          </CardDescription>
        </CardHeader>

        <CardContent className="grid grid-cols-1 gap-5 md:grid-cols-2">
          <div className="space-y-2 md:col-span-2">
            <Label htmlFor="kind">{t("team.kind")}</Label>
            <select
              id="kind"
              className="flex h-9 w-full rounded-md border border-input bg-transparent px-3 py-1 text-sm shadow-sm transition-colors focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-50"
              value={team.kind}
              disabled={saving || !isCreate}
              aria-describedby="kind-help"
              onChange={(event) => {
                return onChange("kind", event.target.value as Team["kind"])
              }}
            >
              <option value="player">{t("team.kindPlayer")}</option>
              <option value="npc">{t("team.kindNpc")}</option>
            </select>
            <p id="kind-help" className="text-xs text-slate-400">
              {t("team.npcDescription")}
            </p>
          </div>

          <div className="logo-preview md:col-span-2">
            <TeamAvatar
              name={team.name || t("common.team")}
              src={team.logo_path}
              size="large"
            />
            <p>
              {t("team.logoPreview")}
              <br />
              {t("team.logoInstructions")}
            </p>
          </div>

          <div className="space-y-2">
            <Label htmlFor="name">{t("team.name")}</Label>
            <Input
              disabled={saving || isNpc}
              id="name"
              required
              maxLength={MAX_TEAM_NAME_LENGTH}
              value={team.name}
              onChange={(event) => {
                return onChange("name", event.target.value)
              }}
            />
          </div>

          <div className="space-y-2">
            <Label htmlFor="ip">{t("team.ip")}</Label>
            <Input
              disabled={saving}
              id="ip"
              required
              value={team.ip}
              onChange={(event) => {
                return onChange("ip", event.target.value)
              }}
            />
          </div>

          <div className="space-y-2">
            <Label htmlFor="token">
              {t("team.token")}{isCreate ? ` ${t("team.serverCreatesToken")}` : ""}
            </Label>
            <Input
              disabled={saving || (isNpc && !isCreate)}
              id="token"
              readOnly={isCreate}
              placeholder={isCreate ? t("team.tokenPlaceholder") : ""}
              maxLength={16}
              value={team.token}
              onChange={(event) => {
                return onChange("token", event.target.value)
              }}
            />
          </div>

          <div className="space-y-2">
            <Label htmlFor="logo_path">{t("team.logoUrl")}</Label>
            <Input
              disabled={saving || isNpc}
              id="logo_path"
              placeholder={t("team.logoPlaceholder")}
              value={team.logo_path}
              onChange={(event) => {
                return onChange("logo_path", event.target.value)
              }}
            />
          </div>

          <div className="flex flex-wrap gap-4 md:col-span-2">
            <label className="flex items-center gap-2 text-xs admin-text-primary">
              <Checkbox
                disabled={saving || isNpc}
                checked={team.highlighted}
                onCheckedChange={(value) => {
                  return onChange("highlighted", Boolean(value))
                }
                }
              />
              {t("team.highlight")}
            </label>

            <label className="flex items-center gap-2 text-xs admin-text-primary">
              <Checkbox
                disabled={saving || (isNpc && isCreate)}
                checked={team.active}
                onCheckedChange={(value) => {
                  return onChange("active", Boolean(value))
                }}
              />
              {t("team.active")}
            </label>
          </div>
        </CardContent>

        <CardFooter className="form-actions mx-6">
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={saving}
            onClick={onCancel}
          >
            {t("common.cancel")}
          </Button>
          <Button type="submit" size="sm" disabled={saving || !canSave}>
            {saving ? t("common.saving") : t("common.save")}
          </Button>
        </CardFooter>
      </Card>
    </form>
  );
}
