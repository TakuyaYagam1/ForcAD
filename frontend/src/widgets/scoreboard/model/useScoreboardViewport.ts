import { useLayoutEffect, type RefObject } from "react";

function pixels(value: string): number {
  return Number.parseFloat(value) || 0;
}

function verticalChrome(element: HTMLElement): number {
  const style = window.getComputedStyle(element);
  return (
    pixels(style.paddingTop) +
    pixels(style.paddingBottom) +
    pixels(style.borderTopWidth) +
    pixels(style.borderBottomWidth)
  );
}

function outerHeight(element: HTMLElement, ignoreTopMargin = false): number {
  const style = window.getComputedStyle(element);
  if (style.display === "none") return 0;
  return (
    element.offsetHeight +
    (ignoreTopMargin ? 0 : pixels(style.marginTop)) +
    pixels(style.marginBottom)
  );
}

export function useScoreboardViewport(
  enabled: boolean,
  pageRef: RefObject<HTMLDivElement | null>,
  teamCount: number,
) {
  useLayoutEffect(() => {
    if (!enabled) return;

    const page = pageRef.current;
    const main = page?.closest<HTMLElement>(".site-main");
    const shell = main?.closest<HTMLElement>(".brand-app");
    const header = shell?.querySelector<HTMLElement>(".site-header");
    const footer = shell?.querySelector<HTMLElement>(".site-footer");
    if (!page || !main || !shell || !header || !footer) return;

    const desktop = window.matchMedia("(min-width: 901px)");
    let frame = 0;

    const reset = () => {
      shell.style.removeProperty("--scoreboard-shell-offset");
      page.style.removeProperty("--scoreboard-table-max-height");
      page.style.removeProperty("--scoreboard-row-height");
    };

    const update = () => {
      frame = 0;
      if (!desktop.matches) {
        reset();
        return;
      }

      // Include an optional demo panel above the app in the viewport budget.
      const offset = Math.max(
        0,
        shell.getBoundingClientRect().top + window.scrollY,
      );
      shell.style.setProperty("--scoreboard-shell-offset", `${offset}px`);

      const blocks = Array.from(page.children).filter(
        (element): element is HTMLElement =>
          element instanceof HTMLElement &&
          !element.classList.contains("scoreboard-reveal"),
      );
      const fixedHeight = blocks.reduce(
        (total, block) =>
          total + outerHeight(block, block.classList.contains("recent-events")),
        0,
      );
      const reveal = page.querySelector<HTMLElement>(".scoreboard-reveal");
      const tableFrame = reveal?.querySelector<HTMLElement>(".table-frame");
      const scroll = tableFrame?.querySelector<HTMLElement>(".table-scroll");
      const head = scroll?.querySelector<HTMLElement>("thead");
      const rows = Array.from(
        scroll?.querySelectorAll<HTMLElement>("tbody tr") ?? [],
      );
      const firstRow = rows[0];
      const empty = reveal?.querySelector<HTMLElement>(".empty-state");

      const available =
        window.innerHeight -
        offset -
        outerHeight(header) -
        outerHeight(footer) -
        verticalChrome(shell) -
        verticalChrome(main) -
        verticalChrome(page) -
        fixedHeight -
        (reveal ? verticalChrome(reveal) : 0) -
        (tableFrame ? verticalChrome(tableFrame) : 0);

      // A short window must still show the header and one row (or an empty state).
      const scrollbar = scroll
        ? Math.max(
            0,
            scroll.offsetHeight - scroll.clientHeight - verticalChrome(scroll),
          )
        : 0;
      // Fill the viewport with all teams. Filtering keeps the same row density.
      // Very short windows retain scrolling instead of shrinking text further.
      if (rows.length && teamCount > 0) {
        const rowHeight = `${Math.max(
          24,
          (Math.floor(available) - (head?.offsetHeight ?? 0) - scrollbar) /
            Math.max(teamCount, rows.length),
        )}px`;
        if (page.style.getPropertyValue("--scoreboard-row-height") !== rowHeight) {
          page.style.setProperty("--scoreboard-row-height", rowHeight);
        }
      }
      const minimum = scroll
        ? (head?.offsetHeight ?? 0) + (firstRow?.offsetHeight ?? 0) + scrollbar
        : empty
          ? (empty.querySelector<HTMLElement>("h2")?.offsetHeight ?? 0) +
            verticalChrome(empty)
          : 0;
      const limit = `${Math.max(minimum, Math.floor(available), 0)}px`;
      if (
        page.style.getPropertyValue("--scoreboard-table-max-height") !== limit
      ) {
        page.style.setProperty("--scoreboard-table-max-height", limit);
      }
    };

    const schedule = () => {
      if (!frame) frame = window.requestAnimationFrame(update);
    };
    const observer = new ResizeObserver(schedule);
    const observeParts = () => {
      observer.disconnect();
      const elements = new Set<Element>([
        page,
        main,
        header,
        footer,
        ...page.children,
        ...page.querySelectorAll(
          ".table-frame, .table-scroll, thead, tbody tr",
        ),
      ]);
      if (shell.parentElement) {
        elements.add(shell.parentElement);
        for (const sibling of shell.parentElement.children)
          elements.add(sibling);
      }
      elements.forEach((element) => observer.observe(element));
    };
    const mutations = new MutationObserver(() => {
      observeParts();
      schedule();
    });

    observeParts();
    mutations.observe(page, { childList: true, subtree: true });
    window.addEventListener("resize", schedule);
    desktop.addEventListener("change", schedule);
    update();

    return () => {
      if (frame) window.cancelAnimationFrame(frame);
      observer.disconnect();
      mutations.disconnect();
      window.removeEventListener("resize", schedule);
      desktop.removeEventListener("change", schedule);
      reset();
    };
  }, [enabled, pageRef, teamCount]);
}
