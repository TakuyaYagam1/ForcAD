import { useRef, useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { isAxiosError } from "axios";
import { changeAdminGameState } from "@/shared/api/generated/endpoints";
import { Flag, LoaderCircle, Pause, Play } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle,
} from "@/components/ui/dialog";
import { useScoreboardStore } from "@/entities/scoreboard/model/store";
import { getLocale } from "@/shared/i18n/index";
import { apiErrorMessage } from "@/shared/lib/apiError";
import {
  queryClient,
  refreshGameQueries,
  setQuerySnapshot,
} from "@/shared/lib/queryClient";
import { useTranslation } from "react-i18next";

type ConfirmedAction = "start_practice" | "start_final" | "finish";
type GameAction = ConfirmedAction | "pause" | "resume";

export function GameControls() {
  const { t } = useTranslation("admin");
  const phase = useScoreboardStore((state) => state.phase);
  const practice = useScoreboardStore((state) => state.practice);
  const canStart = useScoreboardStore((state) => state.canStart);
  const canStartPractice = useScoreboardStore((state) => state.canStartPractice);
  const resetPending = useScoreboardStore((state) => state.resetPending);
  const scheduledStart = useScoreboardStore((state) => state.scheduledStart);
  const generation = useScoreboardStore((state) => state.generation);
  const [confirmationGeneration, setConfirmationGeneration] = useState(0);
  const [confirmation, setConfirmation] = useState<ConfirmedAction | null>(null);
  const [error, setError] = useState<unknown | null>(null);
  const cancelRef = useRef<HTMLButtonElement>(null);
  const action = phase === "paused" ? "resume" : "pause";
  const mutation = useMutation({
    mutationFn: async (next: GameAction) =>
      changeAdminGameState(next, {
        generation:
          next === "pause" || next === "resume"
            ? generation
            : confirmationGeneration,
        ...(next === "finish" || next === "start_final"
          ? { confirm: true }
          : {}),
      }),
    onMutate: async () => {
      setError(null);
      await queryClient.cancelQueries({ queryKey: ["game-runtime"], exact: true });
    },
    onSuccess: (runtime) => {
      setQuerySnapshot(queryClient, ["game-runtime"], runtime);
      useScoreboardStore.getState().setRuntime(runtime);
      setConfirmation(null);
    },
    onError: (cause) => {
      void queryClient.refetchQueries({ queryKey: ["game-runtime"], exact: true });
      setError(cause);
    },
    onSettled: () => refreshGameQueries(queryClient),
    retry: false,
  });
  const disabled = mutation.isPending || phase === "unknown" || resetPending;
  const playing = phase === "running" || phase === "paused";
  const staleConfirmation = confirmation !== null && confirmationGeneration !== generation;
  const starting = confirmation === "start_practice" || confirmation === "start_final";
  const finishLabel = practice ? t("game.finishPractice") : t("game.finish");
  const confirmLabel = confirmation === "start_practice" ? t("game.startPractice")
    : confirmation === "start_final" ? t("game.startFinal") : finishLabel;
  const openConfirmation = (next: ConfirmedAction) => {
    setError(null);
    setConfirmationGeneration(generation);
    setConfirmation(next);
  };
  const scheduledDate = scheduledStart ? new Date(scheduledStart) : null;
  const scheduleLabel = scheduledDate && !Number.isNaN(scheduledDate.getTime())
    ? new Intl.DateTimeFormat(getLocale(), {
        day: "numeric",
        month: "long",
        hour: "2-digit",
        minute: "2-digit",
        timeZoneName: "short",
      }).format(scheduledDate)
    : null;
  const errorMessage = error === null
    ? null
    : isAxiosError(error) && error.response?.status === 409
      ? t("game.errors.stateChanged")
      : apiErrorMessage(error, t("game.errors.commandFailed"));
  return (
    <section className="mb-5" aria-label={t("game.sectionLabel")}>
      <div className="flex flex-wrap items-center justify-end gap-4">
        <div className="flex flex-wrap gap-2">
          <Button variant="outline" disabled={disabled || !playing} onClick={() => mutation.mutate(action)}>
            {mutation.isPending && (mutation.variables === "pause" || mutation.variables === "resume")
              ? <LoaderCircle className="animate-spin" />
              : phase === "paused" ? <Play /> : <Pause />}
            {phase === "paused" ? t("game.resume") : t("game.pause")}
          </Button>
          {canStart && canStartPractice && (
            <Button variant="outline" disabled={disabled}
              onClick={() => openConfirmation("start_practice")}>
              <Play /> {t("game.startPractice")}
            </Button>
          )}
          <Button variant={canStart ? "default" : "destructive"}
            className={canStart ? "bg-emerald-700 text-white hover:bg-emerald-600" : undefined}
            disabled={disabled || (!canStart && !playing)}
            onClick={() => openConfirmation(canStart ? "start_final" : "finish")}>
            {canStart ? <Play /> : <Flag />}
            {canStart ? t("game.startFinal") : finishLabel}
          </Button>
        </div>
      </div>
      {errorMessage && !confirmation && <p className="mt-3 text-sm text-red-300" role="alert">{errorMessage}</p>}
      <Dialog open={confirmation !== null} onOpenChange={(open) => {
        if (!mutation.isPending && !open) setConfirmation(null);
      }}>
        <DialogContent onOpenAutoFocus={(event) => {
          event.preventDefault();
          cancelRef.current?.focus();
        }}>
          <DialogHeader>
            <DialogTitle>{t("game.confirmTitle", { action: confirmLabel })}</DialogTitle>
            <DialogDescription>
              {confirmation === "start_practice"
                ? t("game.practiceConfirmation", {
                    date: scheduleLabel ?? t("game.scheduleUnavailable"),
                  })
                : confirmation === "start_final"
                  ? t("game.finalConfirmation")
                : practice
                  ? t("game.finishPracticeConfirmation")
                  : t("game.finishConfirmation")}
            </DialogDescription>
          </DialogHeader>
          {staleConfirmation && <p className="text-sm text-amber-200" role="alert">
            {t("game.staleConfirmation")}
          </p>}
          {errorMessage && <p className="text-sm text-red-300" role="alert">{errorMessage}</p>}
          <DialogFooter>
            <Button ref={cancelRef} variant="outline" disabled={mutation.isPending}
              onClick={() => setConfirmation(null)}>{t("common.cancel")}</Button>
            <Button variant={starting ? "default" : "destructive"}
              className={starting ? "bg-emerald-700 text-white hover:bg-emerald-600" : undefined}
              disabled={disabled || staleConfirmation || (starting ? !canStart : !playing)
                || (confirmation === "start_practice" && !canStartPractice)}
              onClick={() => { if (confirmation) mutation.mutate(confirmation); }}>
              {mutation.isPending && <LoaderCircle className="animate-spin" />}
              {confirmLabel}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </section>
  );
}
