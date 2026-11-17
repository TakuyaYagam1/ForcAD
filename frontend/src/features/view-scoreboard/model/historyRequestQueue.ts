interface WaitingRequest {
  start(): void;
  abort(): void;
}

/** One queue is shared by all visible scoreboard rows, including remounts. */
export function createHistoryRequestQueue(concurrency = 4) {
  let active = 0;
  const waiting: WaitingRequest[] = [];
  const drain = () => {
    while (active < concurrency && waiting.length) {
      waiting.shift()!.start();
    }
  };
  return async function request<T>(operation: () => Promise<T>, signal: AbortSignal): Promise<T> {
    signal.throwIfAborted();
    await new Promise<void>((resolve, reject) => {
      const entry: WaitingRequest = {
        start() {
          signal.removeEventListener("abort", entry.abort);
          active += 1;
          resolve();
        },
        abort() {
          const index = waiting.indexOf(entry);
          if (index >= 0) {
            waiting.splice(index, 1);
          }
          reject(signal.reason ?? new DOMException("Aborted", "AbortError"));
        },
      };
      signal.addEventListener("abort", entry.abort, {
        once: true
      });
      waiting.push(entry);
      drain();
    });
    try {
      signal.throwIfAborted();
      return await operation();
    } finally {
      active -= 1;
      drain();
    }
  };
}

export const requestScoreboardHistory = createHistoryRequestQueue();
