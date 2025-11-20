import {useEffect, useRef, useState} from "react";
import {createPortal} from "react-dom";
import {cn} from "@/lib/utils";
import {STATUS_COLOR_BY_CODE,} from "@/shared/config/statuses";
import {Table, TableBody, TableCell, TableHead, TableHeader, TableRow,} from "@/components/ui/table";

export interface ScoreboardTeam {
    id: number;
    name: string;
    score: number;
    highlighted?: boolean;
    logo_path?: string;
    ip: string;
}

export interface ScoreboardTask {
    id: number;
    name: string;
}

export interface ScoreboardTeamTask {
    id: number;
    teamId: number;
    taskId: number;
    status: number;
    score: number;
    sla: number;
    stolen: number;
    lost: number;
    message: string;
}

interface ScoreboardTableProps {
    teams: ScoreboardTeam[];
    tasks: ScoreboardTask[];
    teamTasks: ScoreboardTeamTask[];
    /** Клик по имени команды (левая колонка) */
    onTeamClick?: (teamId: number) => void;
    /** Клик по названию таска (хедер) */
    onTaskClick?: (taskId: number) => void;
    /** Клик по ячейке (опционально) */
    onCellClick?: (teamId: number, taskId: number) => void;
}

function getCellColor(status?: number): string | undefined {
    if (!status) return undefined;
    return STATUS_COLOR_BY_CODE[status];
}

/**
 * Небольшой самодельный popover с кружком "i".
 */
function InfoPopover({message}: { message?: string }) {
    const [open, setOpen] = useState(false);
    const [coords, setCoords] = useState<{ top: number; left: number } | null>(
        null
    );
    const [placeBelow, setPlaceBelow] = useState(false);
    const btnRef = useRef<HTMLButtonElement | null>(null);
    const closeTimeout = useRef<number | null>(null);

    // создаём контейнер для портала
    const [container] = useState<HTMLElement | null>(() => {
        if (typeof document === "undefined") return null;
        const el = document.createElement("div");
        return el;
    });

    useEffect(() => {
        if (!container) return;
        document.body.appendChild(container);
        return () => {
            document.body.removeChild(container);
        };
    }, [container]);

    useEffect(() => {
        return () => {
            if (closeTimeout.current != null) {
                window.clearTimeout(closeTimeout.current);
            }
        };
    }, []);

    if (!message) return null;

    const cancelClose = () => {
        if (closeTimeout.current != null) {
            window.clearTimeout(closeTimeout.current);
            closeTimeout.current = null;
        }
    };

    const scheduleClose = () => {
        if (closeTimeout.current != null) {
            window.clearTimeout(closeTimeout.current);
        }
        closeTimeout.current = window.setTimeout(() => {
            setOpen(false);
        }, 120); // небольшая задержка, чтобы не мигало при переходе на поповер
    };

    const openPopover = () => {
        if (!btnRef.current) return;
        const rect = btnRef.current.getBoundingClientRect();

        // координаты правого верхнего угла кнопки
        setCoords({
            top: rect.top,
            left: rect.right,
        });

        // если кнопка близко к верхнему краю — рисуем поповер снизу,
        // иначе сверху (чтобы не уезжал за видимую область)
        const shouldPlaceBelow = rect.top < 80;
        setPlaceBelow(shouldPlaceBelow);

        setOpen(true);
    };

    const popoverNode =
        open && coords && container
            ? createPortal(
                <div
                    onMouseEnter={cancelClose}
                    onMouseLeave={scheduleClose}
                    style={{
                        position: "fixed",
                        top: placeBelow ? coords.top : coords.top,
                        left: coords.left,
                        transform: placeBelow
                            ? "translate(-100%, 0.25rem)" // под кнопкой
                            : "translate(-100%, -100%) translateY(-0.25rem)", // над кнопкой
                        zIndex: 9999,
                    }}
                    className="max-w-xs rounded-lg border border-slate-700 bg-slate-950 px-2 py-1 text-[10px] text-slate-100 shadow-lg"
                >
                    {message}
                </div>,
                container
            )
            : null;

    return (
        <>
            <button
                ref={btnRef}
                type="button"
                className="flex h-4 w-4 items-center justify-center rounded-full border border-slate-300/70 bg-slate-900/80 text-[9px] font-semibold text-slate-100 hover:bg-slate-800/90"
                onMouseEnter={(e) => {
                    e.stopPropagation();
                    cancelClose();
                    openPopover();
                }}
                onMouseLeave={(e) => {
                    e.stopPropagation();
                    scheduleClose();
                }}
            >
                i
            </button>
            {popoverNode}
        </>
    );
}

export function ScoreboardTable({
                                    teams,
                                    tasks,
                                    teamTasks,
                                    onTeamClick,
                                    onTaskClick,
                                    onCellClick,
                                }: ScoreboardTableProps) {
    const hasData = teams.length > 0 && tasks.length > 0;

    if (!hasData) {
        return (
            <div
                className="rounded-2xl border border-dashed border-slate-800/80 bg-slate-950/60 px-4 py-6 text-center text-sm text-slate-400 backdrop-blur">
                Ожидаем данные табло…
            </div>
        );
    }

    return (
        <div
            className="overflow-hidden rounded-2xl border border-slate-800/80 bg-slate-950/80 shadow-xl shadow-indigo-900/40 backdrop-blur">
            <div className="max-h-[70vh] overflow-auto">
                <Table className="min-w-full border-collapse text-sm text-slate-100">
                    <TableHeader className="sticky top-0 z-10 bg-slate-950/90 backdrop-blur">
                        <TableRow className="border-b border-slate-800/80">
                            <TableHead
                                className="w-48 border-r border-slate-800/80 text-left text-xs uppercase tracking-[0.18em] text-slate-400">
                                Team
                            </TableHead>

                            {tasks.map((task) => (
                                <TableHead
                                    key={task.id}
                                    className={cn(
                                        "min-w-[8rem] border-r border-slate-800/60 text-center text-[11px] font-medium uppercase tracking-[0.15em] text-slate-400",
                                        onTaskClick && "cursor-pointer hover:text-slate-100"
                                    )}
                                    title={task.name}
                                    onClick={onTaskClick ? () => onTaskClick(task.id) : undefined}
                                >
                                    <span className="line-clamp-1">{task.name}</span>
                                </TableHead>
                            ))}

                            <TableHead className="w-24 text-right text-xs uppercase tracking-[0.18em] text-slate-400">
                                Score
                            </TableHead>
                        </TableRow>
                    </TableHeader>

                    <TableBody>
                        {teams.map((team, index) => (
                            <TableRow
                                key={team.id}
                                className={cn(
                                    "border-b border-slate-800/60 bg-slate-950/40 transition-colors hover:bg-slate-900/80",
                                    team.highlighted &&
                                    "ring-1 ring-amber-400/70 ring-offset-0 bg-amber-500/10"
                                )}
                            >
                                {/* Team name + place */}
                                <TableCell
                                    className={cn(
                                        "whitespace-nowrap border-r border-slate-800/60 px-3 py-2 text-left text-sm",
                                        onTeamClick && "cursor-pointer hover:text-slate-50"
                                    )}
                                    onClick={onTeamClick ? () => onTeamClick(team.id) : undefined}
                                >
                                    <div className="flex items-center gap-2">
    <span className="w-5 text-xs text-slate-500">
      #{index + 1}
    </span>

                                        {team.logo_path && (
                                            <img
                                                src={team.logo_path}
                                                alt={team.name}
                                                className="h-10 w-10 flex-shrink-0 rounded-sm object-contain"
                                            />
                                        )}

                                        <div className="flex min-w-0 flex-col">
      <span className="truncate font-medium text-slate-100">
        {team.name}
      </span>
                                            <span className="truncate text-[11px] text-slate-100">
        {team.ip}
      </span>
                                        </div>
                                    </div>
                                </TableCell>

                                {/* Per-task cells */}
                                {tasks.map((task) => {
                                    const tt = teamTasks.find(
                                        (t) => t.teamId === team.id && t.taskId === task.id
                                    );

                                    const bg = getCellColor(tt?.status);


                                    const clickable = Boolean(onCellClick);

                                    return (
                                        <TableCell
                                            key={task.id}
                                            className={cn(
                                                "border-r border-slate-800/40 px-2 py-1.5 text-center align-middle text-xs",
                                                clickable && "cursor-pointer hover:brightness-110"
                                            )}
                                            style={
                                                bg
                                                    ? {
                                                        backgroundColor: bg,
                                                    }
                                                    : undefined
                                            }
                                            onClick={
                                                onCellClick
                                                    ? () => onCellClick(team.id, task.id)
                                                    : undefined
                                            }
                                        >
                                            <div className="relative flex flex-col gap-0.5 text-[10px] text-slate-100">
                                                {/* кружок i в правом верхнем углу */}
                                                {tt?.message && (
                                                    <div className="absolute right-1 top-1">
                                                        <InfoPopover message={tt.message}/>
                                                    </div>
                                                )}

                                                <div className="flex items-center justify-between gap-1">
                          <span className="font-mono text-[11px] text-slate-50">
                            {tt ? tt.score.toFixed(2) : "0.00"}
                          </span>

                                                </div>

                                                <div className="flex items-center justify-between gap-1">
                          <span>
                            SLA:{" "}
                              <span className="font-mono">
                              {tt ? tt.sla.toFixed(1) : "0.0"}%
                            </span>
                          </span>
                                                    <span className="font-mono text-amber-200">
                            +{tt?.stolen ?? 0}/-{tt?.lost ?? 0}
                          </span>
                                                </div>
                                            </div>
                                        </TableCell>
                                    );
                                })}

                                {/* Total score */}
                                <TableCell
                                    className="whitespace-nowrap px-3 py-2 text-right align-middle text-sm font-semibold text-slate-50">
                                    {team.score.toFixed(2)}
                                </TableCell>
                            </TableRow>
                        ))}
                    </TableBody>
                </Table>
            </div>
        </div>
    );
}