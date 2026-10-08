// src/app/index.tsx
import React from "react";
import ReactDOM from "react-dom/client";
import { BrowserRouter } from "react-router-dom";
import { QueryClientProvider } from "@tanstack/react-query";
import { GameEventsProvider } from "./providers/GameEventsProvider";
import App from "./App";
import "./styles/tailwind.css";
import "./styles/index.scss";
import "./styles/fidelity.scss";
import { LiveEventsProvider } from "./providers/LiveEventsProvider";
import { DEMO_MODE } from "./demo/mode";
import type { ComponentType } from "react";

import { queryClient } from "@/shared/lib/queryClient";

async function mountApp() {
  let DemoPanel: ComponentType<{ onDataChange: () => void }> | null = null;
  if (DEMO_MODE) {
    const { installDemoData } = await import("./demo/install");
    installDemoData();
    DemoPanel = (await import("./demo/DemoControls")).DemoControls;
  }
  const content = (
    <>
      {DemoPanel && (
        <DemoPanel onDataChange={() => void queryClient.invalidateQueries()} />
      )}
      <App />
    </>
  );
  ReactDOM.createRoot(document.getElementById("root") as HTMLElement).render(
    <React.StrictMode>
      <QueryClientProvider client={queryClient}>
        <BrowserRouter>
          {DEMO_MODE ? (
            content
          ) : (
            <GameEventsProvider>
              <LiveEventsProvider>{content}</LiveEventsProvider>
            </GameEventsProvider>
          )}
        </BrowserRouter>
      </QueryClientProvider>
    </React.StrictMode>,
  );
}

void mountApp();
