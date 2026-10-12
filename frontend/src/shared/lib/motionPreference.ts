export function isMotionAllowed(preference: MediaQueryList): boolean {
  if (
    preference.matches ||
    document.documentElement.dataset.motion === "off" ||
    document.visibilityState === "hidden"
  ) {
    return false;
  }

  try {
    return localStorage.getItem("cf-motion") !== "off";
  } catch {
    return true;
  }
}

export function watchMotionDisable(
  preference: MediaQueryList,
  onDisable: () => void,
): () => void {
  const checkPreference = () => {
    if (!isMotionAllowed(preference)) onDisable();
  };
  const observer = new MutationObserver(checkPreference);
  observer.observe(document.documentElement, {
    attributes: true,
    attributeFilter: ["data-motion"],
  });
  preference.addEventListener("change", checkPreference);
  document.addEventListener("visibilitychange", checkPreference);

  let active = true;
  return () => {
    if (!active) return;
    active = false;
    observer.disconnect();
    preference.removeEventListener("change", checkPreference);
    document.removeEventListener("visibilitychange", checkPreference);
  };
}
