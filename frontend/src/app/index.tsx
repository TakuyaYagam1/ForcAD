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

import { queryClient } from "@/shared/lib/queryClient";
import { applyBrandConfig, applyBrandMetadata } from "./config/brand";
import { i18n, i18nReady } from "@/shared/i18n";

async function mountApp() {
  await i18nReady;
  applyBrandConfig();
  i18n.on("languageChanged", applyBrandMetadata);
  if (DEMO_MODE) {
    const { installDemoData } = await import("./demo/install");
    installDemoData();
  }
  ReactDOM.createRoot(document.getElementById("root") as HTMLElement).render(
    <React.StrictMode>
      <QueryClientProvider client={queryClient}>
        <BrowserRouter>
          {DEMO_MODE ? (
            <App />
          ) : (
            <GameEventsProvider>
              <LiveEventsProvider><App /></LiveEventsProvider>
            </GameEventsProvider>
          )}
        </BrowserRouter>
      </QueryClientProvider>
    </React.StrictMode>,
  );
}

void mountApp();
