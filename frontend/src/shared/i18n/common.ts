export const commonRu = {
  dialog: {
    close: "Закрыть",
  },
  rank: {
    ariaLabel: "Место {{rank}}",
  },
  team: {
    linkLabel: "Команда {{name}}",
    logoAlt: "Логотип {{name}}",
  },
  score: {
    ariaLabel: "{{value}}",
  },
  service: {
    ariaLabel: "{{name}}: {{description}}. Подробнее",
    waitingData: "ожидание данных",
    noData: "Данные сервиса ещё не получены",
    score: "Очки",
    capturedFlags: "Захвачено флагов",
    lostFlags: "Потеряно флагов",
    checkerResponse: "Ответ чекера",
    noCheckerMessage: "Нет сообщения",
  },
  events: {
    capturedFrom: "захватила флаг у",
    points: "очков",
    latest: "Последние события",
    team: "События команды",
    all: "Все события",
    empty: "Захваты флагов появятся здесь по мере поступления событий.",
  },
  gameStatus: {
    running: "Игра идет",
    paused: "Игра приостановлена",
    finished: "Игра завершилась",
    waiting: "Ожидание старта",
    unknown: "Статус игры недоступен",
  },
  status: {
    ok: {
      description: "Сервис работает штатно (UP)",
    },
    corrupt: {
      description: "Повреждённый ответ сервиса (CORRUPT)",
    },
    mumble: {
      description: "Ошибка или неполный ответ сервиса (MUMBLE)",
    },
    down: {
      description: "Сервис недоступен (DOWN)",
    },
    checkerError: {
      description: "Ошибка проверки сервиса (CHECK FAILED)",
    },
  },
};

export const commonEn: typeof commonRu = {
  dialog: {
    close: "Close",
  },
  rank: {
    ariaLabel: "Rank {{rank}}",
  },
  team: {
    linkLabel: "Team {{name}}",
    logoAlt: "{{name}} logo",
  },
  score: {
    ariaLabel: "{{value}}",
  },
  service: {
    ariaLabel: "{{name}}: {{description}}. More details",
    waitingData: "waiting for data",
    noData: "Service data has not been received yet",
    score: "Score",
    capturedFlags: "Flags captured",
    lostFlags: "Flags lost",
    checkerResponse: "Checker response",
    noCheckerMessage: "No message",
  },
  events: {
    capturedFrom: "captured a flag from",
    points: "points",
    latest: "Recent events",
    team: "Team events",
    all: "All events",
    empty: "Flag captures will appear here as events arrive.",
  },
  gameStatus: {
    running: "Game in progress",
    paused: "Game paused",
    finished: "Game finished",
    waiting: "Waiting to start",
    unknown: "Game status is unavailable",
  },
  status: {
    ok: {
      description: "Service is operating normally (UP)",
    },
    corrupt: {
      description: "Service returned corrupted data (CORRUPT)",
    },
    mumble: {
      description: "Service returned an error or incomplete response (MUMBLE)",
    },
    down: {
      description: "Service is unavailable (DOWN)",
    },
    checkerError: {
      description: "Service check failed (CHECK FAILED)",
    },
  },
};
