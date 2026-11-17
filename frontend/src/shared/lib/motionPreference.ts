export function isMotionAllowed(preference: MediaQueryList): boolean {
  return !preference.matches && document.visibilityState !== "hidden" &&
    document.documentElement.dataset.motion !== "off";
}

export function watchMotionDisable(
  preference: MediaQueryList,
  onDisable: () => void,
): () => void {
  const checkPreference = () => {
    if (!isMotionAllowed(preference)) {
      onDisable();
    }
  };
  preference.addEventListener("change", checkPreference);
  document.addEventListener("visibilitychange", checkPreference);
  const observer = new MutationObserver(checkPreference);
  observer.observe(document.documentElement, {
    attributes: true,
    attributeFilter: ["data-motion"],
  });

  let active = true;
  return () => {
    if (!active) {
      return;
    }
    active = false;
    preference.removeEventListener("change", checkPreference);
    document.removeEventListener("visibilitychange", checkPreference);
    observer.disconnect();
  };
}
