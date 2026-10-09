import { useRef, useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { isAxiosError } from "axios";
import { Flag, LoaderCircle, Pause, Play } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle,
} from "@/components/ui/dialog";
import {
  useScoreboardStore, type GameRuntimeStatus,
} from "@/entities/scoreboard/model/store";
import { api } from "@/shared/lib/axios";
import { queryClient, refreshGameQueries } from "@/shared/lib/queryClient";

type ConfirmedAction = "start_practice" | "start_final" | "finish";
type GameAction = ConfirmedAction | "pause" | "resume";

export function GameControls() {
  const phase = useScoreboardStore((state) => state.phase);
  const practice = useScoreboardStore((state) => state.practice);
  const canStart = useScoreboardStore((state) => state.canStart);
  const canStartPractice = useScoreboardStore((state) => state.canStartPractice);
  const resetPending = useScoreboardStore((state) => state.resetPending);
  const scheduledStart = useScoreboardStore((state) => state.scheduledStart);
  const generation = useScoreboardStore((state) => state.generation);
  const [confirmationGeneration, setConfirmationGeneration] = useState(0);
  const [confirmation, setConfirmation] = useState<ConfirmedAction | null>(null);
  const [error, setError] = useState<string | null>(null);
  const cancelRef = useRef<HTMLButtonElement>(null);
  const action = phase === "paused" ? "resume" : "pause";
  const mutation = useMutation({
    mutationFn: async (next: GameAction) => (
      await api.post<GameRuntimeStatus>(`/admin/game/${next}/`,
        {
          generation: next === "pause" || next === "resume" ? generation : confirmationGeneration,
          ...(next === "finish" || next === "start_final" ? { confirm: true } : {}),
        })
    ).data,
    onMutate: async () => {
      setError(null);
      await queryClient.cancelQueries({ queryKey: ["game-runtime"] });
    },
    onSuccess: (runtime) => {
      queryClient.setQueryData(["game-runtime"], runtime);
      useScoreboardStore.getState().setRuntime(runtime);
      setConfirmation(null);
    },
    onError: (cause) => {
      const status = isAxiosError(cause) ? cause.response?.status : undefined;
      setError(status === 409
        ? "Состояние игры изменилось. Обнови страницу перед следующей командой."
        : status === 401 || status === 403
          ? "Сессия администратора завершена. Войди снова."
          : "Не удалось подтвердить результат команды. Проверь статус игры перед повторной попыткой.");
    },
    onSettled: refreshGameQueries,
    retry: false,
  });
  const disabled = mutation.isPending || phase === "unknown" || resetPending;
  const playing = phase === "running" || phase === "paused";
  const staleConfirmation = confirmation !== null && confirmationGeneration !== generation;
  const starting = confirmation === "start_practice" || confirmation === "start_final";
  const finishLabel = practice ? "Завершить тестовую игру" : "Завершить игру";
  const confirmLabel = confirmation === "start_practice" ? "Начать тестовую игру"
    : confirmation === "start_final" ? "Начать финал" : finishLabel;
  const openConfirmation = (next: ConfirmedAction) => {
    setError(null);
    setConfirmationGeneration(generation);
    setConfirmation(next);
  };
  const scheduledDate = scheduledStart ? new Date(scheduledStart) : null;
  const scheduleLabel = scheduledDate?.toLocaleString("ru-RU", {
    day: "numeric", month: "long", hour: "2-digit", minute: "2-digit", timeZoneName: "short",
  });
  return (
    <section className="mb-5 rounded-lg border border-amber-200/15 p-4" aria-label="Управление игрой">
      <div className="flex flex-wrap items-center justify-between gap-4">
        <div className="max-w-xl text-sm text-muted-foreground">
          <p>{resetPending || (practice && phase === "finished")
            ? "Пробная игра завершена. Ждем завершения проверок и очищаем тестовые результаты."
            : practice
              ? "Пробная игра. После завершения ее результаты очистятся. Команды, токены и настройки сохранятся."
              : phase === "finished"
                ? "Финал завершен. Результаты сохранены. Возобновление недоступно."
                : canStart
                  ? "Можно начать финал сейчас. Его результаты сохранятся после завершения."
                  : phase === "paused"
                    ? "Раунды приостановлены, прием флагов закрыт. Можно продолжить игру."
                    : "Пауза временно останавливает новые раунды и прием флагов."}</p>
          {scheduleLabel && (canStart || practice || resetPending) && (
            <p className="mt-1">Старт по расписанию: {scheduleLabel}.</p>
          )}
        </div>
        <div className="flex flex-wrap gap-2">
          <Button variant="outline" disabled={disabled || !playing} onClick={() => mutation.mutate(action)}>
            {mutation.isPending && (mutation.variables === "pause" || mutation.variables === "resume")
              ? <LoaderCircle className="animate-spin" />
              : phase === "paused" ? <Play /> : <Pause />}
            {phase === "paused" ? "Продолжить" : "Приостановить"}
          </Button>
          {canStart && canStartPractice && (
            <Button variant="outline" disabled={disabled}
              onClick={() => openConfirmation("start_practice")}>
              <Play /> Начать тестовую игру
            </Button>
          )}
          <Button variant={canStart ? "default" : "destructive"}
            className={canStart ? "bg-emerald-700 text-white hover:bg-emerald-600" : undefined}
            disabled={disabled || (!canStart && !playing)}
            onClick={() => openConfirmation(canStart ? "start_final" : "finish")}>
            {canStart ? <Play /> : <Flag />}
            {canStart ? "Начать финал" : finishLabel}
          </Button>
        </div>
      </div>
      {error && !confirmation && <p className="mt-3 text-sm text-red-300" role="alert">{error}</p>}
      <Dialog open={confirmation !== null} onOpenChange={(open) => {
        if (!mutation.isPending && !open) setConfirmation(null);
      }}>
        <DialogContent onOpenAutoFocus={(event) => {
          event.preventDefault();
          cancelRef.current?.focus();
        }}>
          <DialogHeader>
            <DialogTitle>{confirmLabel}?</DialogTitle>
            <DialogDescription>
              {confirmation === "start_practice"
                ? `Проверки и прием флагов начнутся сейчас. Старт по расписанию остается ${scheduleLabel}. После завершения теста или при наступлении этой даты тестовые очки, флаги и история очистятся. Команды, токены и настройки сохранятся.`
                : confirmation === "start_final"
                  ? "Официальная игра начнется сейчас с первого раунда. Лимит раундов из config.yml действует и при ручном старте. После завершения очки, флаги и история сохранятся. Дата старта из config.yml больше не перезапустит эту игру. Завершенный финал возобновить нельзя."
                : practice
                  ? "Прием флагов и новые раунды прекратятся. После завершения отправленных проверок тестовые очки, флаги и история очистятся. Борд вернется в ожидание официального старта."
                  : "Прием флагов и запуск новых раундов прекратятся. Уже отправленные проверки завершатся, после чего обновится итоговый рейтинг. Очки, флаги и история сохранятся. Продолжить эту игру будет нельзя. Если нужен перерыв, используй паузу."}
            </DialogDescription>
          </DialogHeader>
          {staleConfirmation && <p className="text-sm text-amber-200" role="alert">
            Игра уже изменилась. Закрой окно и проверь ее состояние.
          </p>}
          {error && <p className="text-sm text-red-300" role="alert">{error}</p>}
          <DialogFooter>
            <Button ref={cancelRef} variant="outline" disabled={mutation.isPending}
              onClick={() => setConfirmation(null)}>Отмена</Button>
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
