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

type GameAction = "pause" | "resume" | "finish";

export function GameControls() {
  const phase = useScoreboardStore((state) => state.phase);
  const round = useScoreboardStore((state) => state.runtimeRound);
  const [confirmFinish, setConfirmFinish] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const cancelRef = useRef<HTMLButtonElement>(null);
  const action = phase === "paused" ? "resume" : "pause";
  const mutation = useMutation({
    mutationFn: async (next: GameAction) => (
      await api.post<GameRuntimeStatus>(`/admin/game/${next}/`,
        next === "finish" ? { confirm: true } : {})
    ).data,
    onMutate: async () => {
      setError(null);
      await queryClient.cancelQueries({ queryKey: ["game-runtime"] });
    },
    onSuccess: (runtime) => {
      queryClient.setQueryData(["game-runtime"], runtime);
      useScoreboardStore.getState().setRuntime(runtime);
      setConfirmFinish(false);
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
  const disabled = mutation.isPending || phase === "unknown" || phase === "finished";
  return (
    <section className="mb-5 rounded-lg border border-amber-200/15 p-4" aria-label="Управление игрой">
      <div className="flex flex-wrap items-center justify-between gap-4">
        <p className="text-sm text-muted-foreground">
          {phase === "finished"
            ? "Игра завершена. Возобновление недоступно."
            : phase === "paused"
              ? "Раунды приостановлены, прием флагов закрыт. Можно продолжить игру."
              : "Пауза временно останавливает новые раунды и прием флагов."}
        </p>
        <div className="flex flex-wrap gap-2">
          <Button variant="outline" disabled={disabled} onClick={() => mutation.mutate(action)}>
            {mutation.isPending && mutation.variables !== "finish"
              ? <LoaderCircle className="animate-spin" />
              : phase === "paused" ? <Play /> : <Pause />}
            {phase === "paused" ? "Продолжить" : "Приостановить"}
          </Button>
          <Button variant="destructive" disabled={disabled || !round} onClick={() => {
            setError(null);
            setConfirmFinish(true);
          }}>
            <Flag /> Завершить игру
          </Button>
        </div>
      </div>
      {error && !confirmFinish && <p className="mt-3 text-sm text-red-300" role="alert">{error}</p>}
      <Dialog open={confirmFinish} onOpenChange={(open) => {
        if (!mutation.isPending) setConfirmFinish(open);
      }}>
        <DialogContent onOpenAutoFocus={(event) => {
          event.preventDefault();
          cancelRef.current?.focus();
        }}>
          <DialogHeader>
            <DialogTitle>Завершить игру?</DialogTitle>
            <DialogDescription>
              Прием флагов и запуск новых раундов прекратятся. Уже отправленные
              проверки завершатся, после чего обновится итоговый рейтинг.
              Продолжить эту игру будет нельзя. Если нужен перерыв, используй паузу.
            </DialogDescription>
          </DialogHeader>
          {error && <p className="text-sm text-red-300" role="alert">{error}</p>}
          <DialogFooter>
            <Button ref={cancelRef} variant="outline" disabled={mutation.isPending}
              onClick={() => setConfirmFinish(false)}>Отмена</Button>
            <Button variant="destructive" disabled={disabled || !round}
              onClick={() => mutation.mutate("finish")}>
              {mutation.isPending && <LoaderCircle className="animate-spin" />}
              Завершить игру
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </section>
  );
}
