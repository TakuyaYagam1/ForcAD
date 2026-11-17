import { useId, useState, type FormEvent, type ReactNode } from "react";
import { useMutation } from "@tanstack/react-query";
import { isAxiosError } from "axios";
import { LoaderCircle, Settings } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useScoreboardStore } from "@/entities/scoreboard/model/store";
import {
  getGetAdminGameSettingsQueryKey,
  updateAdminGameSettings,
  useGetAdminGameSettings,
} from "@/shared/api/generated/endpoints";
import type {
  AdminGameSettings,
  AdminGameSettingsUpdate,
} from "@/shared/api/generated/schemas";
import { apiErrorMessage } from "@/shared/lib/apiError";
import {
  queryClient,
  refreshGameQueries,
  setQuerySnapshot,
} from "@/shared/lib/queryClient";
import { useAdminNotification } from "@/shared/ui/admin-notification/useAdminNotification";

type GameMode = "classic" | "blitz";
type FieldName =
  | "rounds"
  | "flagLifetime"
  | "gameHardness"
  | "roundTime"
  | "extraChecks"
  | "mode"
  | "timezone"
  | "startTime";
type ValidationIssue =
  | "positiveInteger"
  | "hardness"
  | "extraChecks"
  | "roundLimit"
  | "required"
  | "timezoneTooLong"
  | "invalidDate"
  | "invalidMode";

type SettingsForm = {
  rounds: string;
  flagLifetime: string;
  gameHardness: string;
  inflation: boolean;
  volgaAttacksMode: boolean;
  roundTime: string;
  extraChecks: string;
  mode: string;
  timezone: string;
  startTime: string;
};

type BuildResult =
  | {
    body: AdminGameSettingsUpdate;
    errors: Partial<Record<FieldName, ValidationIssue>>;
    formError?: never;
  }
  | {
    body?: never;
    errors: Partial<Record<FieldName, ValidationIssue>>;
    formError: "lockedFields" | "roundsLocked" | "unchanged";
  };

const INT32_MAX = 2_147_483_647;
const EXTRA_CHECKS_MAX = 8;
const MIN_VALID_HARDNESS = 1 + Number.EPSILON;
const selectClassName = "flex h-9 w-full rounded-md border border-input bg-transparent px-3 py-1 text-sm shadow-sm transition-colors focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-50";
const formKeys = Object.freeze([
  "rounds",
  "flagLifetime",
  "gameHardness",
  "inflation",
  "volgaAttacksMode",
  "roundTime",
  "extraChecks",
  "mode",
  "timezone",
  "startTime",
] satisfies Array<keyof SettingsForm>);

function isGameMode(value: string): value is GameMode {
  return value === "classic" || value === "blitz";
}

function normalizeIso(value: string): string | null {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return null;
  }
  return date.toISOString();
}

function localInputToIso(value: string): string | null {
  const match = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})(?::(\d{2}))?$/.exec(value);
  if (!match) {
    return null;
  }
  const [, yearText, monthText, dayText, hourText, minuteText, secondText = "0"] = match;
  const year = Number(yearText);
  const month = Number(monthText);
  const day = Number(dayText);
  const hour = Number(hourText);
  const minute = Number(minuteText);
  const second = Number(secondText);
  const date = new Date(year, month - 1, day, hour, minute, second);
  if (date.getFullYear() !== year
    || date.getMonth() !== month - 1
    || date.getDate() !== day
    || date.getHours() !== hour
    || date.getMinutes() !== minute
    || date.getSeconds() !== second) {
    return null;
  }
  return date.toISOString();
}

function isoToLocalInput(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return "";
  }
  const localTime = new Date(date.getTime() - date.getTimezoneOffset() * 60_000);
  return localTime.toISOString().slice(0, 16);
}

function settingsToForm(settings: AdminGameSettings): SettingsForm {
  return {
    rounds: settings.config.rounds === null ? "" : String(settings.config.rounds),
    flagLifetime: String(settings.config.flag_lifetime),
    gameHardness: String(settings.config.game_hardness),
    inflation: settings.config.inflation,
    volgaAttacksMode: settings.config.volga_attacks_mode,
    roundTime: String(settings.config.round_time),
    extraChecks: String(settings.extra_checks),
    mode: settings.config.mode,
    timezone: settings.config.timezone,
    startTime: isoToLocalInput(settings.config.start_time),
  };
}

function parseInteger(
  value: string,
  minimum: number,
  maximum = INT32_MAX,
): number | null {
  if (!/^-?\d+$/.test(value.trim())) {
    return null;
  }
  const parsed = Number(value);
  return Number.isInteger(parsed) && parsed >= minimum && parsed <= maximum ? parsed : null;
}

function buildUpdate(
  settings: AdminGameSettings,
  form: SettingsForm,
  permissions: {
    canEditAll: boolean;
    canEditRounds: boolean;
  },
): BuildResult {
  const initial = settingsToForm(settings);
  const { canEditAll, canEditRounds } = permissions;
  const errors: Partial<Record<FieldName, ValidationIssue>> = {};

  if (!canEditAll && formKeys.some((key) => {
    return key !== "rounds" && form[key] !== initial[key]
  })) {
    return {
      errors,
      formError: "lockedFields",
    };
  }
  if (!canEditRounds && form.rounds !== initial.rounds) {
    return {
      errors,
      formError: "roundsLocked",
    };
  }

  let rounds = settings.config.rounds;
  if (canEditRounds) {
    if (form.rounds.trim() === "") {
      rounds = null;
    } else {
      const value = parseInteger(form.rounds, 1);
      if (value === null) {
        errors.rounds = "positiveInteger";
      } else if (settings.config.real_round !== null && value <= settings.config.real_round) {
        errors.rounds = "roundLimit";
      } else {
        rounds = value;
      }
    }
  }

  let flagLifetime = settings.config.flag_lifetime;
  let gameHardness = settings.config.game_hardness;
  let inflation = settings.config.inflation;
  let volgaAttacksMode = settings.config.volga_attacks_mode;
  let roundTime = settings.config.round_time;
  let extraChecks = settings.extra_checks;
  let mode = settings.config.mode;
  let timezone = settings.config.timezone;
  let startTime = normalizeIso(settings.config.start_time);

  if (canEditAll) {
    const parsedFlagLifetime = parseInteger(form.flagLifetime, 1);
    const parsedHardness = Number(form.gameHardness);
    const parsedRoundTime = parseInteger(form.roundTime, 1);
    const parsedExtraChecks = parseInteger(form.extraChecks, 0, EXTRA_CHECKS_MAX);
    const trimmedTimezone = form.timezone.trim();

    if (parsedFlagLifetime === null) {
      errors.flagLifetime = "positiveInteger";
    } else {
      flagLifetime = parsedFlagLifetime;
    }
    if (form.gameHardness.trim() === ""
      || !Number.isFinite(parsedHardness)
      || parsedHardness <= 1) {
      errors.gameHardness = "hardness";
    } else {
      gameHardness = parsedHardness;
    }
    if (parsedRoundTime === null) {
      errors.roundTime = "positiveInteger";
    } else {
      roundTime = parsedRoundTime;
    }
    if (parsedExtraChecks === null) {
      errors.extraChecks = "extraChecks";
    } else {
      extraChecks = parsedExtraChecks;
    }
    if (!isGameMode(form.mode)) {
      errors.mode = "invalidMode";
    } else {
      mode = form.mode;
    }
    if (trimmedTimezone === "") {
      errors.timezone = "required";
    } else if (trimmedTimezone.length > 128) {
      errors.timezone = "timezoneTooLong";
    } else {
      timezone = trimmedTimezone;
    }
    if (form.startTime === initial.startTime) {
      startTime = normalizeIso(settings.config.start_time);
    } else {
      startTime = localInputToIso(form.startTime);
    }
    if (startTime === null) {
      errors.startTime = "invalidDate";
    }
    inflation = form.inflation;
    volgaAttacksMode = form.volgaAttacksMode;
  }

  if (!Number.isInteger(flagLifetime) || flagLifetime < 1 || flagLifetime > INT32_MAX) {
    errors.flagLifetime = "positiveInteger";
  }
  if (!Number.isFinite(gameHardness) || gameHardness <= 1) {
    errors.gameHardness = "hardness";
  }
  if (!Number.isInteger(roundTime) || roundTime < 1 || roundTime > INT32_MAX) {
    errors.roundTime = "positiveInteger";
  }
  if (!Number.isInteger(extraChecks) || extraChecks < 0 || extraChecks > EXTRA_CHECKS_MAX) {
    errors.extraChecks = "extraChecks";
  }
  if (timezone.trim() === "") {
    errors.timezone = "required";
  } else if (timezone.length > 128) {
    errors.timezone = "timezoneTooLong";
  }
  if (!isGameMode(mode)) {
    errors.mode = "invalidMode";
  }
  if (startTime === null) {
    errors.startTime = "invalidDate";
  }
  if (Object.keys(errors).length > 0 || !isGameMode(mode) || startTime === null) {
    return {
      errors,
      formError: canEditAll ? "lockedFields" : "unchanged",
    };
  }

  const body: AdminGameSettingsUpdate = {
    generation: settings.runtime.generation,
    rounds,
    flag_lifetime: flagLifetime,
    game_hardness: gameHardness,
    inflation,
    volga_attacks_mode: volgaAttacksMode,
    round_time: roundTime,
    extra_checks: extraChecks,
    mode,
    timezone,
    start_time: startTime,
  };
  const baselineStartTime = normalizeIso(settings.config.start_time);
  const changed = body.rounds !== settings.config.rounds
    || body.flag_lifetime !== settings.config.flag_lifetime
    || body.game_hardness !== settings.config.game_hardness
    || body.inflation !== settings.config.inflation
    || body.volga_attacks_mode !== settings.config.volga_attacks_mode
    || body.round_time !== settings.config.round_time
    || body.extra_checks !== settings.extra_checks
    || body.mode !== settings.config.mode
    || body.timezone !== settings.config.timezone
    || body.start_time !== baselineStartTime;

  if (!changed) {
    return {
      errors,
      formError: "unchanged",
    };
  }
  return {
    body,
    errors,
  };
}

function Field({
  id,
  label,
  help,
  error,
  children,
}: {
  id: string;
  label: string;
  help: string;
  error?: string;
  children: ReactNode;
}) {
  return (
    <div className="space-y-1.5">
      <Label htmlFor={id}>{label}</Label>
      {children}
      <p id={`${id}-help`} className="text-xs text-muted-foreground">{help}</p>
      {error && <p id={`${id}-error`} className="text-xs text-red-300" role="alert">{error}</p>}
    </div>
  );
}

export function GameSettings() {
  const { t } = useTranslation("admin");
  const notify = useAdminNotification();
  const idPrefix = useId();
  const [open, setOpen] = useState(false);
  const [formOverride, setFormOverride] = useState<SettingsForm | null>(null);
  const [fieldErrors, setFieldErrors] = useState<Partial<Record<FieldName, ValidationIssue>>>({});
  const [formError, setFormError] = useState<string | null>(null);
  const livePhase = useScoreboardStore((state) => {
    return state.phase
  });
  const liveCanStart = useScoreboardStore((state) => {
    return state.canStart
  });
  const browserTimeZone = Intl.DateTimeFormat().resolvedOptions().timeZone;
  const settingsKey = getGetAdminGameSettingsQueryKey();
  const settingsQuery = useGetAdminGameSettings({
    query: {
      enabled: open,
      refetchOnWindowFocus: false,
      retry: false,
      staleTime: 0,
    },
  }, queryClient);
  const mutation = useMutation({
    mutationFn: (body: AdminGameSettingsUpdate) => {
      return updateAdminGameSettings(body)
    },
    onMutate: () => {
      setFormError(null);
    },
    onSuccess: (settings) => {
      setQuerySnapshot(queryClient, settingsKey, settings);
      useScoreboardStore.getState().setRuntime(settings.runtime);
      setFormOverride(null);
      setFieldErrors({});
      notify(t("gameSettings.success"));
      refreshGameQueries(queryClient);
      void Promise.all([
        queryClient.refetchQueries({
          queryKey: settingsKey,
          exact: true,
        }),
        queryClient.refetchQueries({
          queryKey: ["game-runtime"],
          exact: true,
        }),
        queryClient.refetchQueries({
          queryKey: ["scoreboardInit"],
          exact: true,
        }),
      ]);
    },
    onError: (cause) => {
      if (isAxiosError(cause) && cause.response?.status === 409) {
        setFormError(t("gameSettings.errors.stale"));
        setFormOverride(null);
        setFieldErrors({});
        void settingsQuery.refetch();
        return;
      }
      setFormError(apiErrorMessage(cause, t("gameSettings.errors.updateFailed")));
    },
    retry: false,
  });

  const updateForm = <Key extends keyof SettingsForm>(key: Key, value: SettingsForm[Key]) => {
    setFormOverride((current) => {
      const source = current ?? (settingsQuery.data
        ? settingsToForm(settingsQuery.data)
        : null);
      return source ? {
        ...source,
        [key]: value,
      } : null
    });
    setFieldErrors((current) => {
      const next = {
        ...current,
      };
      delete next[key as FieldName];
      return next;
    });
    setFormError(null);
  };

  const close = () => {
    if (mutation.isPending) {
      return;
    }
    setOpen(false);
    setFormOverride(null);
    setFieldErrors({});
    setFormError(null);
    mutation.reset();
  };

  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const settings = settingsQuery.data;
    const form = formOverride ?? (settings ? settingsToForm(settings) : null);
    if (!settings || !form || mutation.isPending) {
      return;
    }
    const canEditAll = settings.runtime.can_start && (livePhase === "unknown" || liveCanStart);
    const canEditRounds = settings.runtime.phase !== "finished" && livePhase !== "finished";
    const result = buildUpdate(settings, form, {
      canEditAll,
      canEditRounds,
    });
    setFieldErrors(result.errors);
    if (!result.body) {
      const hasFieldErrors = Object.keys(result.errors).length > 0;
      setFormError(hasFieldErrors
        ? t("gameSettings.errors.invalidFields")
        : t(`gameSettings.errors.${result.formError}`));
      return;
    }
    mutation.mutate(result.body);
  };

  const settings = settingsQuery.data;
  const canEditAll = settings?.runtime.can_start === true
    && (livePhase === "unknown" || liveCanStart);
  const canEditRounds = settings?.runtime.phase !== "finished" && livePhase !== "finished";
  const initialForm = settings ? settingsToForm(settings) : null;
  const form = formOverride ?? initialForm;
  const dirty = initialForm && form
    ? formKeys.some((key) => {
      return initialForm[key] !== form[key]
    })
    : false;
  const validationMessage = (field: FieldName) => {
    const issue = fieldErrors[field];
    if (!issue) {
      return undefined;
    }
    return issue === "roundLimit"
      ? t("gameSettings.validation.roundLimit", {
        round: settings?.config.real_round ?? 0,
      })
      : t(`gameSettings.validation.${issue}`);
  };
  const fieldId = (field: FieldName) => {
    return `${idPrefix}-${field}`
  };

  return (
    <section aria-label={t("gameSettings.sectionLabel")}>
      <Button
        type="button"
        variant="outline"
        data-testid="game-settings-trigger"
        onClick={() => {
          setFormOverride(null);
          setFieldErrors({});
          setFormError(null);
          setOpen(true);
        }}
      >
        <Settings />
        {t("gameSettings.open")}
      </Button>
      <Dialog open={open} onOpenChange={(nextOpen) => {
        if (!nextOpen) {
          close();
        }
      }}>
        <DialogContent className="admin-dialog admin-game-settings max-h-[92dvh] max-w-3xl overflow-y-auto">
          <DialogHeader>
            <DialogTitle>{t("gameSettings.title")}</DialogTitle>
            <DialogDescription>{t("gameSettings.description")}</DialogDescription>
          </DialogHeader>

          {settingsQuery.isPending && (
            <div className="flex items-center gap-2 py-8 text-sm text-muted-foreground" role="status">
              <LoaderCircle className="animate-spin" />
              {t("gameSettings.loading")}
            </div>
          )}
          {settingsQuery.isError && !settings && (
            <div className="space-y-3 py-4">
              <p className="text-sm text-red-300" role="alert">
                {apiErrorMessage(settingsQuery.error, t("gameSettings.errors.loadFailed"))}
              </p>
              <Button type="button" variant="outline" onClick={() => {
                return void settingsQuery.refetch()
              }}>
                {t("gameSettings.retry")}
              </Button>
            </div>
          )}
          {settingsQuery.isError && settings && (
            <p className="text-sm text-red-300" role="alert">
              {apiErrorMessage(settingsQuery.error, t("gameSettings.errors.loadFailed"))}
            </p>
          )}

          {settings && form && (
            <form noValidate onSubmit={submit} data-testid="game-settings-form" className="space-y-5">
              <div className="admin-settings-warning">
                {t("gameSettings.resetWarning")}
              </div>
              {!canEditAll && (
                <p className="text-sm text-muted-foreground">
                  {canEditRounds ? t("gameSettings.runtimeLocked") : t("gameSettings.finishedLocked")}
                </p>
              )}

              <div className="grid gap-5 sm:grid-cols-2">
                <Field
                  id={fieldId("rounds")}
                  label={t("gameSettings.rounds.label")}
                  help={t("gameSettings.rounds.help")}
                  error={validationMessage("rounds")}
                >
                  <Input
                    id={fieldId("rounds")}
                    type="number"
                    min={settings.config.real_round === null
                      ? 1
                      : Math.max(1, settings.config.real_round + 1)}
                    max={INT32_MAX}
                    step={1}
                    value={form.rounds}
                    disabled={!canEditRounds || mutation.isPending}
                    aria-describedby={`${fieldId("rounds")}-help`}
                    aria-invalid={fieldErrors.rounds ? true : undefined}
                    data-testid="game-settings-rounds"
                    onChange={(event) => {
                      return updateForm("rounds", event.target.value)
                    }}
                  />
                </Field>
                <Field
                  id={fieldId("roundTime")}
                  label={t("gameSettings.roundTime.label")}
                  help={t("gameSettings.roundTime.help")}
                  error={validationMessage("roundTime")}
                >
                  <Input
                    id={fieldId("roundTime")}
                    type="number"
                    min={1}
                    max={INT32_MAX}
                    step={1}
                    value={form.roundTime}
                    disabled={!canEditAll || mutation.isPending}
                    aria-describedby={`${fieldId("roundTime")}-help`}
                    aria-invalid={fieldErrors.roundTime ? true : undefined}
                    data-testid="game-settings-round-time"
                    onChange={(event) => {
                      return updateForm("roundTime", event.target.value)
                    }}
                  />
                </Field>
                <Field
                  id={fieldId("flagLifetime")}
                  label={t("gameSettings.flagLifetime.label")}
                  help={t("gameSettings.flagLifetime.help")}
                  error={validationMessage("flagLifetime")}
                >
                  <Input
                    id={fieldId("flagLifetime")}
                    type="number"
                    min={1}
                    max={INT32_MAX}
                    step={1}
                    value={form.flagLifetime}
                    disabled={!canEditAll || mutation.isPending}
                    aria-describedby={`${fieldId("flagLifetime")}-help`}
                    aria-invalid={fieldErrors.flagLifetime ? true : undefined}
                    onChange={(event) => {
                      return updateForm("flagLifetime", event.target.value)
                    }}
                  />
                </Field>
                <Field
                  id={fieldId("extraChecks")}
                  label={t("gameSettings.extraChecks.label")}
                  help={t("gameSettings.extraChecks.help")}
                  error={validationMessage("extraChecks")}
                >
                  <Input
                    id={fieldId("extraChecks")}
                    type="number"
                    min={0}
                    max={EXTRA_CHECKS_MAX}
                    step={1}
                    value={form.extraChecks}
                    disabled={!canEditAll || mutation.isPending}
                    aria-describedby={`${fieldId("extraChecks")}-help`}
                    aria-invalid={fieldErrors.extraChecks ? true : undefined}
                    data-testid="game-settings-extra-checks"
                    onChange={(event) => {
                      return updateForm("extraChecks", event.target.value)
                    }}
                  />
                </Field>
                <Field
                  id={fieldId("gameHardness")}
                  label={t("gameSettings.gameHardness.label")}
                  help={t("gameSettings.gameHardness.help")}
                  error={validationMessage("gameHardness")}
                >
                  <Input
                    id={fieldId("gameHardness")}
                    type="number"
                    min={MIN_VALID_HARDNESS}
                    max={Number.MAX_VALUE}
                    step="any"
                    value={form.gameHardness}
                    disabled={!canEditAll || mutation.isPending}
                    aria-describedby={`${fieldId("gameHardness")}-help`}
                    aria-invalid={fieldErrors.gameHardness ? true : undefined}
                    data-testid="game-settings-hardness"
                    onChange={(event) => {
                      return updateForm("gameHardness", event.target.value)
                    }}
                  />
                </Field>
                <Field
                  id={fieldId("mode")}
                  label={t("gameSettings.mode.label")}
                  help={t("gameSettings.mode.help")}
                  error={validationMessage("mode")}
                >
                  <select
                    id={fieldId("mode")}
                    className={selectClassName}
                    value={form.mode}
                    disabled={!canEditAll || mutation.isPending}
                    aria-describedby={`${fieldId("mode")}-help`}
                    aria-invalid={fieldErrors.mode ? true : undefined}
                    data-testid="game-settings-mode"
                    onChange={(event) => {
                      return updateForm("mode", event.target.value)
                    }}
                  >
                    <option value="classic">{t("gameSettings.mode.classic")}</option>
                    <option value="blitz">{t("gameSettings.mode.blitz")}</option>
                  </select>
                </Field>
                <Field
                  id={fieldId("timezone")}
                  label={t("gameSettings.timezone.label")}
                  help={t("gameSettings.timezone.help")}
                  error={validationMessage("timezone")}
                >
                  <Input
                    id={fieldId("timezone")}
                    type="text"
                    maxLength={128}
                    value={form.timezone}
                    disabled={!canEditAll || mutation.isPending}
                    aria-describedby={`${fieldId("timezone")}-help`}
                    aria-invalid={fieldErrors.timezone ? true : undefined}
                    onChange={(event) => {
                      return updateForm("timezone", event.target.value)
                    }}
                  />
                </Field>
                <Field
                  id={fieldId("startTime")}
                  label={t("gameSettings.startTime.label")}
                  help={t("gameSettings.startTime.help", {
                    timeZone: browserTimeZone,
                  })}
                  error={validationMessage("startTime")}
                >
                  <Input
                    id={fieldId("startTime")}
                    type="datetime-local"
                    step={60}
                    value={form.startTime}
                    disabled={!canEditAll || mutation.isPending}
                    aria-describedby={`${fieldId("startTime")}-help`}
                    aria-invalid={fieldErrors.startTime ? true : undefined}
                    data-testid="game-settings-start-time"
                    onChange={(event) => {
                      return updateForm("startTime", event.target.value)
                    }}
                  />
                </Field>
              </div>

              <div className="grid gap-4 sm:grid-cols-2">
                <div className="flex items-start gap-3 rounded-md border p-3">
                  <Checkbox
                    id={`${idPrefix}-inflation`}
                    checked={form.inflation}
                    disabled={!canEditAll || mutation.isPending}
                    aria-describedby={`${idPrefix}-inflation-help`}
                    onCheckedChange={(checked) => {
                      return updateForm("inflation", checked === true)
                    }}
                  />
                  <div className="space-y-1">
                    <Label htmlFor={`${idPrefix}-inflation`}>{t("gameSettings.inflation.label")}</Label>
                    <p id={`${idPrefix}-inflation-help`} className="text-xs text-muted-foreground">
                      {t("gameSettings.inflation.help")}
                    </p>
                  </div>
                </div>
                <div className="flex items-start gap-3 rounded-md border p-3">
                  <Checkbox
                    id={`${idPrefix}-volga-attacks`}
                    checked={form.volgaAttacksMode}
                    disabled={!canEditAll || mutation.isPending}
                    aria-describedby={`${idPrefix}-volga-attacks-help`}
                    onCheckedChange={(checked) => {
                      return updateForm("volgaAttacksMode", checked === true)
                    }}
                  />
                  <div className="space-y-1">
                    <Label htmlFor={`${idPrefix}-volga-attacks`}>{t("gameSettings.volgaAttacksMode.label")}</Label>
                    <p id={`${idPrefix}-volga-attacks-help`} className="text-xs text-muted-foreground">
                      {t("gameSettings.volgaAttacksMode.help")}
                    </p>
                  </div>
                </div>
              </div>

              {formError && <p className="text-sm text-red-300" role="alert">{formError}</p>}
              <DialogFooter>
                <Button
                  type="button"
                  variant="outline"
                  disabled={mutation.isPending}
                  data-testid="game-settings-cancel"
                  onClick={close}
                >
                  {t("gameSettings.cancel")}
                </Button>
                <Button
                  type="submit"
                  disabled={mutation.isPending || !dirty || (!canEditAll && !canEditRounds)}
                  data-testid="game-settings-submit"
                >
                  {mutation.isPending && <LoaderCircle className="animate-spin" />}
                  {mutation.isPending ? t("gameSettings.saving") : t("gameSettings.save")}
                </Button>
              </DialogFooter>
            </form>
          )}
        </DialogContent>
      </Dialog>
    </section>
  );
}
