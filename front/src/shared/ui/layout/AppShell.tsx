import type {ReactNode} from "react";
import {Link} from "react-router-dom";
import {Separator} from "@/components/ui/separator";
import {CosmicBackground} from "@/shared/ui/backgrounds/CosmicBackground";

interface AppShellProps {
    children: ReactNode;
}

export function AppShell({children}: AppShellProps) {
    return (
        <CosmicBackground>
            <header className="border-b border-slate-800/60 bg-slate-950/70 backdrop-blur">
                <div className="mx-auto flex max-w-6xl items-center justify-between px-4 py-3">
                    {/* Лого + надпись FinalSibCTF2025 */}
                    <Link
                        to="/"
                        className="flex items-center gap-3 hover:opacity-90 transition-opacity"
                    >
                        <div className="flex items-center gap-3">
                            {/* Иконка-«чип» слева */}
                            <div
                                className="flex h-9 w-9 items-center justify-center rounded-xl border border-indigo-500/50 bg-indigo-500/10 shadow-[0_0_30px_rgba(79,70,229,0.45)]">
              <span className="text-[11px] font-semibold tracking-[0.18em] uppercase text-indigo-200">
                AD
              </span>
                            </div>

                            {/* Текстовая часть логотипа */}
                            <div className="flex flex-col leading-tight">
              <span
                  className="bg-gradient-to-r from-sky-300 via-indigo-300 to-fuchsia-300 bg-clip-text text-[11px] font-semibold uppercase tracking-[0.45em] text-transparent">
                FinalSibCTF2025
              </span>
                                <span className="text-[11px] text-slate-400">
                Attack Defence platform
              </span>
                            </div>
                        </div>
                    </Link>

                    {/* Индикатор live справа — можно оставить как был */}
                    <Link
                        to="/live"
                        className="flex items-center gap-2 text-xs text-slate-400 hover:text-slate-100 transition-colors"
                    >
                        <div className="flex items-center gap-2 text-xs text-slate-400">
                            <span className="h-2 w-2 rounded-full bg-emerald-400 animate-pulse"/>
                            live
                        </div>
                    </Link>
                </div>

                <Separator className="border-slate-800/60"/>
            </header>

            <main className="mx-auto flex min-h-[calc(100vh-3.5rem)] max-w-6xl flex-col px-4 py-6">
                {children}
            </main>
        </CosmicBackground>
    );
}
