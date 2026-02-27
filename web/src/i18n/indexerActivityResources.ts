export const indexerActivityResources = {
  en: {
    title: "Scheduler Activity",
    summary: {
      running: "{{count}} running",
      queued: "{{count}} queued",
      cooldown: "{{count}} cooldown",
      workers: "{{inUse}}/{{total}} workers",
    },
    sections: {
      running: "Running ({{count}})",
      queued: "Queued ({{count}})",
      andMore: "...and {{count}} more",
      rateLimited: "Rate Limited ({{count}})",
    },
    empty: {
      noActivity: "No active tasks or rate limits",
    },
    priority: {
      interactive: "interactive",
      rss: "rss",
      completion: "completion",
      background: "background",
    },
    badges: {
      rss: "RSS",
    },
    cooldown: {
      ready: "Ready",
      timeLeft: "{{time}} left",
    },
  },
  "zh-CN": {
    title: "调度活动",
    summary: {
      running: "运行中 {{count}}",
      queued: "排队 {{count}}",
      cooldown: "冷却 {{count}}",
      workers: "工作线程 {{inUse}}/{{total}}",
    },
    sections: {
      running: "运行中（{{count}}）",
      queued: "排队中（{{count}}）",
      andMore: "...以及另外 {{count}} 项",
      rateLimited: "限速中（{{count}}）",
    },
    empty: {
      noActivity: "当前没有活动任务或限速项",
    },
    priority: {
      interactive: "交互",
      rss: "rss",
      completion: "完成",
      background: "后台",
    },
    badges: {
      rss: "RSS",
    },
    cooldown: {
      ready: "就绪",
      timeLeft: "剩余 {{time}}",
    },
  },
  ja: {
    title: "スケジューラー稼働状況",
    summary: {
      running: "実行中 {{count}}",
      queued: "待機中 {{count}}",
      cooldown: "クールダウン {{count}}",
      workers: "ワーカー {{inUse}}/{{total}}",
    },
    sections: {
      running: "実行中（{{count}}）",
      queued: "キュー（{{count}}）",
      andMore: "...他 {{count}} 件",
      rateLimited: "レート制限中（{{count}}）",
    },
    empty: {
      noActivity: "アクティブなタスクまたはレート制限はありません",
    },
    priority: {
      interactive: "interactive",
      rss: "rss",
      completion: "completion",
      background: "background",
    },
    badges: {
      rss: "RSS",
    },
    cooldown: {
      ready: "準備完了",
      timeLeft: "残り {{time}}",
    },
  },
  "pt-BR": {
    title: "Atividade do Agendador",
    summary: {
      running: "{{count}} em execução",
      queued: "{{count}} na fila",
      cooldown: "{{count}} em cooldown",
      workers: "{{inUse}}/{{total}} workers",
    },
    sections: {
      running: "Em execução ({{count}})",
      queued: "Na fila ({{count}})",
      andMore: "...e mais {{count}}",
      rateLimited: "Com rate limit ({{count}})",
    },
    empty: {
      noActivity: "Sem tarefas ativas ou limites de taxa",
    },
    priority: {
      interactive: "interactive",
      rss: "rss",
      completion: "completion",
      background: "background",
    },
    badges: {
      rss: "RSS",
    },
    cooldown: {
      ready: "Pronto",
      timeLeft: "faltam {{time}}",
    },
  },
  "es-419": {
    title: "Actividad del programador",
    summary: {
      running: "{{count}} en ejecución",
      queued: "{{count}} en cola",
      cooldown: "{{count}} en enfriamiento",
      workers: "{{inUse}}/{{total}} workers",
    },
    sections: {
      running: "En ejecución ({{count}})",
      queued: "En cola ({{count}})",
      andMore: "...y {{count}} más",
      rateLimited: "Con límite de tasa ({{count}})",
    },
    empty: {
      noActivity: "No hay tareas activas ni límites de tasa",
    },
    priority: {
      interactive: "interactivo",
      rss: "rss",
      completion: "finalización",
      background: "segundo plano",
    },
    badges: {
      rss: "RSS",
    },
    cooldown: {
      ready: "Listo",
      timeLeft: "quedan {{time}}",
    },
  },
  fr: {
    title: "Activité du planificateur",
    summary: {
      running: "{{count}} en cours",
      queued: "{{count}} en file d'attente",
      cooldown: "{{count}} en cooldown",
      workers: "{{inUse}}/{{total}} workers",
    },
    sections: {
      running: "En cours ({{count}})",
      queued: "En file d'attente ({{count}})",
      andMore: "...et encore {{count}}",
      rateLimited: "Limités par débit ({{count}})",
    },
    empty: {
      noActivity: "Aucune tâche active ni limite de débit",
    },
    priority: {
      interactive: "interactif",
      rss: "rss",
      completion: "finalisation",
      background: "arrière-plan",
    },
    badges: {
      rss: "RSS",
    },
    cooldown: {
      ready: "Prêt",
      timeLeft: "{{time}} restantes",
    },
  },
  ko: {
    title: "스케줄러 활동",
    summary: {
      running: "실행 중 {{count}}개",
      queued: "대기 중 {{count}}개",
      cooldown: "쿨다운 {{count}}개",
      workers: "워커 {{inUse}}/{{total}}개",
    },
    sections: {
      running: "실행 중 ({{count}})",
      queued: "대기열 ({{count}})",
      andMore: "...외 {{count}}개",
      rateLimited: "속도 제한 중 ({{count}})",
    },
    empty: {
      noActivity: "활성 작업이나 속도 제한이 없습니다",
    },
    priority: {
      interactive: "인터랙티브",
      rss: "rss",
      completion: "완료",
      background: "백그라운드",
    },
    badges: {
      rss: "RSS",
    },
    cooldown: {
      ready: "준비됨",
      timeLeft: "{{time}} 남음",
    },
  },
  de: {
    title: "Scheduler-Aktivität",
    summary: {
      running: "{{count}} läuft",
      queued: "{{count}} in Warteschlange",
      cooldown: "{{count}} im Cooldown",
      workers: "{{inUse}}/{{total}} Worker",
    },
    sections: {
      running: "Laufend ({{count}})",
      queued: "Warteschlange ({{count}})",
      andMore: "...und {{count}} weitere",
      rateLimited: "Rate-limitiert ({{count}})",
    },
    empty: {
      noActivity: "Keine aktiven Aufgaben oder Rate-Limits",
    },
    priority: {
      interactive: "interactive",
      rss: "rss",
      completion: "completion",
      background: "background",
    },
    badges: {
      rss: "RSS",
    },
    cooldown: {
      ready: "Bereit",
      timeLeft: "noch {{time}}",
    },
  },
} as const
