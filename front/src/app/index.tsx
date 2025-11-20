// src/app/index.tsx
import React from "react";
import ReactDOM from "react-dom/client";
import { BrowserRouter } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { GameEventsProvider } from "./providers/GameEventsProvider";
import App from "./App";
import "./styles/index.scss";
import { LiveEventsProvider } from "./providers/LiveEventsProvider";

const queryClient = new QueryClient();

ReactDOM.createRoot(document.getElementById("root") as HTMLElement).render(
  <React.StrictMode>
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <GameEventsProvider>
          <LiveEventsProvider>
            <App />
          </LiveEventsProvider>
        </GameEventsProvider>
      </BrowserRouter>
    </QueryClientProvider>
  </React.StrictMode>
);
