export function isMotionAllowed(preference: MediaQueryList): boolean {
  return !preference.matches && document.visibilityState !== "hidden";
}

export function watchMotionDisable(
  preference: MediaQueryList,
  onDisable: () => void,
): () => void {
  const checkPreference = () => {
    if (!isMotionAllowed(preference)) onDisable();
  };
  preference.addEventListener("change", checkPreference);
  document.addEventListener("visibilitychange", checkPreference);

  let active = true;
  return () => {
    if (!active) return;
    active = false;
    preference.removeEventListener("change", checkPreference);
    document.removeEventListener("visibilitychange", checkPreference);
  };
}
