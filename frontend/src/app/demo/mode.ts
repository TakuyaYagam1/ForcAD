const STORAGE_KEY = "forcad-demo-mode";

function readDemoMode() {
  const requested = new URLSearchParams(window.location.search).get("demo");
  if (requested === "1" || requested === "0") {
    try {
      sessionStorage.setItem(STORAGE_KEY, requested === "1" ? "on" : "off");
    } catch {
      // The explicit URL also works when browser storage is unavailable.
    }
    return requested === "1";
  }
  try {
    const stored = sessionStorage.getItem(STORAGE_KEY);
    if (stored === "on" || stored === "off") return stored === "on";
  } catch {
    // Starting Vite in demo mode needs no browser storage.
  }
  return import.meta.env.MODE === "demo";
}

export const DEMO_MODE = readDemoMode();

export function leaveDemoMode() {
  const url = new URL(window.location.href);
  url.searchParams.set("demo", "0");
  try {
    sessionStorage.setItem(STORAGE_KEY, "off");
    sessionStorage.removeItem("forcad-demo-auth");
  } catch {
    // The URL disables demo mode even when storage is unavailable.
  }
  window.location.assign(url.href);
}
