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
    score: "FP",
    capturedFlags: "Захвачено флагов",
    lostFlags: "Потерянные флаги",
    checkerHistory: "История чекера",
    round: "Раунд {{round}}",
    roundColumn: "Раунд",
    statusColumn: "Статус",
    messageColumn: "Ответ чекера",
    gainedFp: "FP, заработанные командой за захваты",
    opponentFp: "FP, заработанные соперниками на флагах команды. При инфляции могут превышать списание.",
    loadingCheckerHistory: "Загружаем историю…",
    checkerHistoryError: "Не удалось загрузить историю чекера",
    noCheckerHistory: "Для этого сервиса пока нет опубликованных результатов",
    noStatusData: "Нет данных",
    unknownStatus: "Неизвестный статус",
    roundNotStarted: "Игра ещё не запущена",
    checkerHistoryUnavailable: "История чекера недоступна",
    retry: "Повторить",
  },
  events: {
    captureLead: "Команда",
    capturedFrom: "захватила флаг у",
    onService: "в сервисе",
    earned: "и получила",
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
    score: "FP",
    capturedFlags: "Flags captured",
    lostFlags: "Flags lost",
    checkerHistory: "Checker history",
    round: "Round {{round}}",
    roundColumn: "Round",
    statusColumn: "Status",
    messageColumn: "Checker response",
    gainedFp: "FP earned by this team for capturing flags",
    opponentFp: "FP opponents earned from this team's flags. With inflation, this can exceed the team's loss.",
    loadingCheckerHistory: "Loading history…",
    checkerHistoryError: "Could not load checker history",
    noCheckerHistory: "No results have been published for this service yet",
    noStatusData: "No data",
    unknownStatus: "Unknown status",
    roundNotStarted: "The game has not started",
    checkerHistoryUnavailable: "Checker history is unavailable",
    retry: "Retry",
  },
  events: {
    captureLead: "Team",
    capturedFrom: "captured a flag from",
    onService: "on",
    earned: "and earned",
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
