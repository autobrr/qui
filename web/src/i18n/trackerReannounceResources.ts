export const trackerReannounceResources = {
  en: {
    header: {
      title: "Automatic Tracker Reannounce",
      tooltip: "qBittorrent does not retry failed announces quickly. When a tracker is slow to register a new upload or returns an error, you can end up waiting. qui handles this automatically without spamming trackers.",
      descriptionPrefix: "qui monitors",
      stalled: "stalled",
      descriptionSuffix: "torrents and reannounces them when no tracker is healthy.",
      scanInterval: "Background scan runs every {{seconds}} seconds.",
    },
    tabs: {
      settings: "Settings",
      activityLog: "Activity Log",
    },
    sections: {
      timingAndBehavior: "Timing & Behavior",
      scopeAndFiltering: "Scope & Filtering",
    },
    values: {
      enabled: "Enabled",
      disabled: "Disabled",
      include: "Include",
      exclude: "Exclude",
      instance: "Instance",
      notAvailable: "N/A",
    },
    placeholders: {
      selectInstance: "Select instance",
      selectCategories: "Select categories...",
      selectTags: "Select tags...",
      selectTrackerDomains: "Select tracker domains...",
    },
    fields: {
      initialWait: {
        label: "Initial Wait",
        description: "Seconds before first check",
        tooltip: "How long to wait after a torrent is added before checking status. This gives the tracker time to register it naturally. Minimum 5 seconds.",
      },
      retryInterval: {
        label: "Retry Interval",
        description: "Seconds between retries",
        tooltip: "How often to retry inside one reannounce attempt. With Quick Retry enabled, this also becomes the cooldown between scans. Minimum 5 seconds.",
      },
      maxTorrentAge: {
        label: "Max Torrent Age",
        description: "Stop monitoring after (s)",
        tooltip: "Stops monitoring torrents older than this value (in seconds). Helps avoid endlessly checking old dead torrents. Minimum 60 seconds.",
      },
      maxRetries: {
        label: "Max Retries",
        description: "Retry attempts per torrent",
        tooltip: "Maximum consecutive retries in one scan cycle. Each scan can retry up to this count before waiting for the next cycle. Slow trackers may need up to 50 retries (7s interval is about 6 minutes). Range: 1-50.",
      },
      quickRetry: {
        label: "Quick Retry",
        description: "Use Retry Interval for cooldown instead of 2 minutes",
        tooltip: "Uses the Retry Interval as cooldown between scans instead of the default 2 minutes. Useful when trackers are slow to register uploads. qui still waits for tracker updates and does not spam.",
      },
      monitorAll: {
        label: "Monitor All Stalled Torrents",
        descriptionLine1: "If enabled, monitors everything except excluded items.",
        descriptionLine2: "If disabled, only monitors items matching the include rules below.",
      },
      categories: {
        label: "Categories",
      },
      tags: {
        label: "Tags",
      },
      trackerDomains: {
        label: "Tracker Domains",
      },
    },
    actions: {
      saveChanges: "Save Changes",
      saving: "Saving...",
      refresh: "Refresh",
    },
    states: {
      instanceNotFound: "Instance not found. Please close and reopen the dialog.",
      failedLoadActivity: "Failed to load activity",
      checkConnection: "Check connection to the instance.",
      loadingActivity: "Loading activity...",
      noActivityYet: "No activity recorded yet.",
      activityWillAppear: "Events will appear here when stalled torrents are detected.",
    },
    activity: {
      title: "Recent Activity",
      enabledDescription: "Real-time log of reannounce attempts and results.",
      disabledDescription: "Monitoring is disabled. No new activity will be recorded.",
      hideSkipped: "Hide skipped",
      copyHash: "Copy hash",
      labels: {
        trackers: "Trackers",
        reason: "Reason",
      },
      outcomes: {
        succeeded: "succeeded",
        failed: "failed",
        skipped: "skipped",
      },
    },
    toasts: {
      settingsSaved: "Settings saved successfully.",
      instanceMissingTitle: "Instance missing",
      instanceMissingDescription: "Please close and reopen the dialog.",
      monitoringUpdated: "Tracker monitoring updated",
      updateFailed: "Update failed",
      unableUpdateSettings: "Unable to update settings",
      monitoringDisabled: "Monitoring disabled",
      hashCopied: "Hash copied",
    },
  },
  "zh-CN": {
    header: {
      title: "自动 Tracker 重通告",
      tooltip: "qBittorrent 不会快速重试失败的通告。若 Tracker 注册新上传较慢或返回错误，你可能会一直等待。qui 会自动处理，同时避免对 Tracker 造成刷请求。",
      descriptionPrefix: "qui 会监控",
      stalled: "卡住的",
      descriptionSuffix: "种子，并在没有健康 Tracker 时触发重新通告。",
      scanInterval: "后台扫描每 {{seconds}} 秒运行一次。",
    },
    tabs: {
      settings: "设置",
      activityLog: "活动日志",
    },
    sections: {
      timingAndBehavior: "时序与行为",
      scopeAndFiltering: "范围与筛选",
    },
    values: {
      enabled: "已启用",
      disabled: "已禁用",
      include: "包含",
      exclude: "排除",
      instance: "实例",
      notAvailable: "N/A",
    },
    placeholders: {
      selectInstance: "选择实例",
      selectCategories: "选择分类...",
      selectTags: "选择标签...",
      selectTrackerDomains: "选择 Tracker 域名...",
    },
    fields: {
      initialWait: {
        label: "初始等待",
        description: "首次检查前等待秒数",
        tooltip: "种子添加后等待多久再开始检查状态。给 Tracker 留出自然注册时间。最小 5 秒。",
      },
      retryInterval: {
        label: "重试间隔",
        description: "每次重试之间的秒数",
        tooltip: "单次重通告过程中的重试频率。启用“快速重试”后，该值也会作为扫描间冷却时间。最小 5 秒。",
      },
      maxTorrentAge: {
        label: "种子最大监控时长",
        description: "超过此秒数后停止监控",
        tooltip: "超过该时长（秒）的种子将不再监控，可避免反复检查长期失效任务。最小 60 秒。",
      },
      maxRetries: {
        label: "最大重试次数",
        description: "每个种子的重试次数",
        tooltip: "单次扫描周期内的最大连续重试次数。达到上限后等待下一轮扫描。慢 Tracker 可能需要最多 50 次重试（7 秒间隔约 6 分钟）。范围：1-50。",
      },
      quickRetry: {
        label: "快速重试",
        description: "使用“重试间隔”作为冷却，而不是 2 分钟",
        tooltip: "将扫描间冷却从默认 2 分钟改为“重试间隔”。适合上传注册慢的 Tracker。qui 仍会等待 Tracker 更新，不会刷请求。",
      },
      monitorAll: {
        label: "监控所有卡住种子",
        descriptionLine1: "启用后会监控除排除项外的全部内容。",
        descriptionLine2: "禁用后仅监控符合下方包含规则的内容。",
      },
      categories: {
        label: "分类",
      },
      tags: {
        label: "标签",
      },
      trackerDomains: {
        label: "Tracker 域名",
      },
    },
    actions: {
      saveChanges: "保存更改",
      saving: "保存中...",
      refresh: "刷新",
    },
    states: {
      instanceNotFound: "未找到实例。请关闭并重新打开此对话框。",
      failedLoadActivity: "加载活动失败",
      checkConnection: "请检查与实例的连接。",
      loadingActivity: "正在加载活动...",
      noActivityYet: "尚无活动记录。",
      activityWillAppear: "检测到卡住种子后，事件会显示在这里。",
    },
    activity: {
      title: "最近活动",
      enabledDescription: "实时显示重通告尝试及结果。",
      disabledDescription: "监控已禁用。不会记录新的活动。",
      hideSkipped: "隐藏跳过项",
      copyHash: "复制哈希",
      labels: {
        trackers: "Tracker",
        reason: "原因",
      },
      outcomes: {
        succeeded: "成功",
        failed: "失败",
        skipped: "已跳过",
      },
    },
    toasts: {
      settingsSaved: "设置已成功保存。",
      instanceMissingTitle: "实例不存在",
      instanceMissingDescription: "请关闭并重新打开此对话框。",
      monitoringUpdated: "Tracker 监控已更新",
      updateFailed: "更新失败",
      unableUpdateSettings: "无法更新设置",
      monitoringDisabled: "监控已禁用",
      hashCopied: "哈希已复制",
    },
  },
  ja: {
    header: {
      title: "トラッカー再アナウンス自動化",
      tooltip: "qBittorrent は失敗したアナウンスを素早く再試行しません。トラッカー側の登録遅延やエラー時に待ち続けることがあります。qui がトラッカーを過剰に叩かず自動で処理します。",
      descriptionPrefix: "qui は",
      stalled: "停滞中",
      descriptionSuffix: "のトレントを監視し、健全なトラッカーがない場合に再アナウンスします。",
      scanInterval: "バックグラウンドスキャンは {{seconds}} 秒ごとに実行されます。",
    },
    tabs: {
      settings: "設定",
      activityLog: "アクティビティログ",
    },
    sections: {
      timingAndBehavior: "タイミングと挙動",
      scopeAndFiltering: "対象範囲とフィルタ",
    },
    values: {
      enabled: "有効",
      disabled: "無効",
      include: "含める",
      exclude: "除外",
      instance: "インスタンス",
      notAvailable: "N/A",
    },
    placeholders: {
      selectInstance: "インスタンスを選択",
      selectCategories: "カテゴリを選択...",
      selectTags: "タグを選択...",
      selectTrackerDomains: "トラッカードメインを選択...",
    },
    fields: {
      initialWait: {
        label: "初回待機",
        description: "初回チェックまでの秒数",
        tooltip: "トレント追加後、状態チェックまで待つ時間です。トラッカーに自然登録される余裕を与えます。最小 5 秒。",
      },
      retryInterval: {
        label: "再試行間隔",
        description: "再試行間の秒数",
        tooltip: "1 回の再アナウンス試行内で再試行する間隔です。クイック再試行有効時はスキャン間のクールダウンにも使われます。最小 5 秒。",
      },
      maxTorrentAge: {
        label: "監視する最大経過時間",
        description: "この秒数を超えたら監視停止",
        tooltip: "この秒数を超えた古いトレントの監視を止めます。恒久的に死んだトレントへの無駄なチェックを防ぎます。最小 60 秒。",
      },
      maxRetries: {
        label: "最大再試行回数",
        description: "トレントごとの再試行回数",
        tooltip: "1 スキャン内での連続再試行の上限です。上限に達すると次サイクルまで待機します。遅いトラッカーでは最大 50 回が必要な場合があります（7 秒間隔で約 6 分）。範囲: 1-50。",
      },
      quickRetry: {
        label: "クイック再試行",
        description: "2 分ではなく再試行間隔をクールダウンに使用",
        tooltip: "スキャン間のクールダウンを既定の 2 分ではなく再試行間隔にします。登録が遅いトラッカー向けです。qui は更新待ちを守り、過剰送信しません。",
      },
      monitorAll: {
        label: "停滞中トレントをすべて監視",
        descriptionLine1: "有効時は、除外項目を除くすべてを監視します。",
        descriptionLine2: "無効時は、下の含める条件に一致した項目のみ監視します。",
      },
      categories: {
        label: "カテゴリ",
      },
      tags: {
        label: "タグ",
      },
      trackerDomains: {
        label: "トラッカードメイン",
      },
    },
    actions: {
      saveChanges: "変更を保存",
      saving: "保存中...",
      refresh: "更新",
    },
    states: {
      instanceNotFound: "インスタンスが見つかりません。ダイアログを閉じて開き直してください。",
      failedLoadActivity: "アクティビティの読み込みに失敗しました",
      checkConnection: "インスタンスへの接続を確認してください。",
      loadingActivity: "アクティビティを読み込み中...",
      noActivityYet: "まだアクティビティはありません。",
      activityWillAppear: "停滞中トレントが検出されると、ここにイベントが表示されます。",
    },
    activity: {
      title: "最近のアクティビティ",
      enabledDescription: "再アナウンス試行と結果をリアルタイムで表示します。",
      disabledDescription: "監視は無効です。新しいアクティビティは記録されません。",
      hideSkipped: "スキップを非表示",
      copyHash: "ハッシュをコピー",
      labels: {
        trackers: "トラッカー",
        reason: "理由",
      },
      outcomes: {
        succeeded: "成功",
        failed: "失敗",
        skipped: "スキップ",
      },
    },
    toasts: {
      settingsSaved: "設定を保存しました。",
      instanceMissingTitle: "インスタンスがありません",
      instanceMissingDescription: "ダイアログを閉じて開き直してください。",
      monitoringUpdated: "トラッカー監視を更新しました",
      updateFailed: "更新に失敗しました",
      unableUpdateSettings: "設定を更新できませんでした",
      monitoringDisabled: "監視を無効化しました",
      hashCopied: "ハッシュをコピーしました",
    },
  },
  "pt-BR": {
    header: {
      title: "Reannounce Automático de Tracker",
      tooltip: "O qBittorrent não tenta novamente anúncios com falha tão rápido. Quando o tracker demora para registrar um upload novo ou retorna erro, você pode ficar esperando. O qui resolve isso automaticamente sem enviar spam ao tracker.",
      descriptionPrefix: "O qui monitora torrents",
      stalled: "travados",
      descriptionSuffix: "e faz reannounce quando nenhum tracker está saudável.",
      scanInterval: "A varredura em segundo plano roda a cada {{seconds}} segundos.",
    },
    tabs: {
      settings: "Configurações",
      activityLog: "Log de Atividade",
    },
    sections: {
      timingAndBehavior: "Tempo e Comportamento",
      scopeAndFiltering: "Escopo e Filtros",
    },
    values: {
      enabled: "Ativado",
      disabled: "Desativado",
      include: "Incluir",
      exclude: "Excluir",
      instance: "Instância",
      notAvailable: "N/A",
    },
    placeholders: {
      selectInstance: "Selecione a instância",
      selectCategories: "Selecione categorias...",
      selectTags: "Selecione tags...",
      selectTrackerDomains: "Selecione domínios de tracker...",
    },
    fields: {
      initialWait: {
        label: "Espera Inicial",
        description: "Segundos antes da primeira checagem",
        tooltip: "Tempo de espera após adicionar o torrent antes de checar o status. Dá tempo para o tracker registrar naturalmente. Mínimo de 5 segundos.",
      },
      retryInterval: {
        label: "Intervalo de Tentativa",
        description: "Segundos entre tentativas",
        tooltip: "Frequência de tentativas dentro de uma rodada de reannounce. Com Retry Rápido ativo, também vira o cooldown entre varreduras. Mínimo de 5 segundos.",
      },
      maxTorrentAge: {
        label: "Idade Máxima do Torrent",
        description: "Parar de monitorar após (s)",
        tooltip: "Para de monitorar torrents mais antigos que este valor (em segundos). Evita verificar indefinidamente torrents antigos e mortos. Mínimo de 60 segundos.",
      },
      maxRetries: {
        label: "Máximo de Tentativas",
        description: "Tentativas por torrent",
        tooltip: "Máximo de tentativas consecutivas em um único ciclo de varredura. Depois disso, espera o próximo ciclo. Trackers lentos podem precisar de até 50 tentativas (intervalo de 7s é cerca de 6 minutos). Faixa: 1-50.",
      },
      quickRetry: {
        label: "Retry Rápido",
        description: "Usar o Intervalo de Tentativa como cooldown em vez de 2 minutos",
        tooltip: "Usa o Intervalo de Tentativa como cooldown entre varreduras em vez do padrão de 2 minutos. Útil em trackers lentos para registrar uploads. O qui ainda espera atualização e não envia spam.",
      },
      monitorAll: {
        label: "Monitorar Todos os Torrents Travados",
        descriptionLine1: "Se ativado, monitora tudo exceto os itens excluídos.",
        descriptionLine2: "Se desativado, monitora apenas itens que batem com as regras de inclusão abaixo.",
      },
      categories: {
        label: "Categorias",
      },
      tags: {
        label: "Tags",
      },
      trackerDomains: {
        label: "Domínios de Tracker",
      },
    },
    actions: {
      saveChanges: "Salvar Alterações",
      saving: "Salvando...",
      refresh: "Atualizar",
    },
    states: {
      instanceNotFound: "Instância não encontrada. Feche e abra o diálogo novamente.",
      failedLoadActivity: "Falha ao carregar atividade",
      checkConnection: "Verifique a conexão com a instância.",
      loadingActivity: "Carregando atividade...",
      noActivityYet: "Nenhuma atividade registrada ainda.",
      activityWillAppear: "Os eventos aparecerão aqui quando torrents travados forem detectados.",
    },
    activity: {
      title: "Atividade Recente",
      enabledDescription: "Log em tempo real das tentativas de reannounce e resultados.",
      disabledDescription: "O monitoramento está desativado. Nenhuma atividade nova será registrada.",
      hideSkipped: "Ocultar ignorados",
      copyHash: "Copiar hash",
      labels: {
        trackers: "Trackers",
        reason: "Motivo",
      },
      outcomes: {
        succeeded: "sucesso",
        failed: "falha",
        skipped: "ignorado",
      },
    },
    toasts: {
      settingsSaved: "Configurações salvas com sucesso.",
      instanceMissingTitle: "Instância ausente",
      instanceMissingDescription: "Feche e abra o diálogo novamente.",
      monitoringUpdated: "Monitoramento de tracker atualizado",
      updateFailed: "Falha ao atualizar",
      unableUpdateSettings: "Não foi possível atualizar as configurações",
      monitoringDisabled: "Monitoramento desativado",
      hashCopied: "Hash copiado",
    },
  },
  de: {
    header: {
      title: "Automatisches Tracker-Reannounce",
      tooltip: "qBittorrent wiederholt fehlgeschlagene Announces nicht schnell genug. Wenn ein Tracker neue Uploads verzögert registriert oder Fehler liefert, wartest du oft unnötig. qui übernimmt das automatisch, ohne Tracker zu spammen.",
      descriptionPrefix: "qui überwacht",
      stalled: "hängende",
      descriptionSuffix: "Torrents und reannounced sie, wenn kein Tracker gesund ist.",
      scanInterval: "Der Hintergrund-Scan läuft alle {{seconds}} Sekunden.",
    },
    tabs: {
      settings: "Einstellungen",
      activityLog: "Aktivitätsprotokoll",
    },
    sections: {
      timingAndBehavior: "Timing & Verhalten",
      scopeAndFiltering: "Bereich & Filter",
    },
    values: {
      enabled: "Aktiviert",
      disabled: "Deaktiviert",
      include: "Einschließen",
      exclude: "Ausschließen",
      instance: "Instanz",
      notAvailable: "N/V",
    },
    placeholders: {
      selectInstance: "Instanz auswählen",
      selectCategories: "Kategorien auswählen...",
      selectTags: "Tags auswählen...",
      selectTrackerDomains: "Tracker-Domains auswählen...",
    },
    fields: {
      initialWait: {
        label: "Initiale Wartezeit",
        description: "Sekunden bis zur ersten Prüfung",
        tooltip: "Wartezeit nach dem Hinzufügen eines Torrents, bevor der Status geprüft wird. So kann der Tracker den Upload normal registrieren. Minimum 5 Sekunden.",
      },
      retryInterval: {
        label: "Retry-Intervall",
        description: "Sekunden zwischen Wiederholungen",
        tooltip: "Wie oft innerhalb eines einzelnen Reannounce-Versuchs erneut versucht wird. Mit Schnell-Wiederholung wird dieser Wert auch zum Cooldown zwischen Scans. Minimum 5 Sekunden.",
      },
      maxTorrentAge: {
        label: "Maximales Torrent-Alter",
        description: "Monitoring stoppen nach (s)",
        tooltip: "Stoppt die Überwachung von Torrents, die älter als dieser Wert (in Sekunden) sind. Verhindert endlose Prüfungen bei dauerhaft toten Torrents. Minimum 60 Sekunden.",
      },
      maxRetries: {
        label: "Maximale Wiederholungen",
        description: "Wiederholversuche pro Torrent",
        tooltip: "Maximale aufeinanderfolgende Wiederholungen in einem Scan-Zyklus. Danach wird bis zum nächsten Zyklus gewartet. Langsame Tracker brauchen teils bis zu 50 Versuche (bei 7s etwa 6 Minuten). Bereich: 1-50.",
      },
      quickRetry: {
        label: "Schnell-Wiederholung",
        description: "Retry-Intervall statt 2 Minuten als Cooldown verwenden",
        tooltip: "Verwendet das Retry-Intervall als Cooldown zwischen Scans statt der Standard-2 Minuten. Hilfreich bei langsamen Trackern. qui wartet trotzdem auf Updates und spammt nicht.",
      },
      monitorAll: {
        label: "Alle hängenden Torrents überwachen",
        descriptionLine1: "Wenn aktiviert, wird alles außer ausgeschlossenen Einträgen überwacht.",
        descriptionLine2: "Wenn deaktiviert, nur Einträge, die den Include-Regeln unten entsprechen.",
      },
      categories: {
        label: "Kategorien",
      },
      tags: {
        label: "Tags",
      },
      trackerDomains: {
        label: "Tracker-Domains",
      },
    },
    actions: {
      saveChanges: "Änderungen speichern",
      saving: "Wird gespeichert...",
      refresh: "Aktualisieren",
    },
    states: {
      instanceNotFound: "Instanz nicht gefunden. Bitte Dialog schließen und erneut öffnen.",
      failedLoadActivity: "Aktivität konnte nicht geladen werden",
      checkConnection: "Verbindung zur Instanz prüfen.",
      loadingActivity: "Aktivität wird geladen...",
      noActivityYet: "Noch keine Aktivität aufgezeichnet.",
      activityWillAppear: "Ereignisse erscheinen hier, sobald hängende Torrents erkannt werden.",
    },
    activity: {
      title: "Letzte Aktivität",
      enabledDescription: "Echtzeitprotokoll der Reannounce-Versuche und Ergebnisse.",
      disabledDescription: "Monitoring ist deaktiviert. Es wird keine neue Aktivität aufgezeichnet.",
      hideSkipped: "Übersprungene ausblenden",
      copyHash: "Hash kopieren",
      labels: {
        trackers: "Tracker",
        reason: "Grund",
      },
      outcomes: {
        succeeded: "erfolgreich",
        failed: "fehlgeschlagen",
        skipped: "übersprungen",
      },
    },
    toasts: {
      settingsSaved: "Einstellungen erfolgreich gespeichert.",
      instanceMissingTitle: "Instanz fehlt",
      instanceMissingDescription: "Bitte Dialog schließen und erneut öffnen.",
      monitoringUpdated: "Tracker-Monitoring aktualisiert",
      updateFailed: "Aktualisierung fehlgeschlagen",
      unableUpdateSettings: "Einstellungen konnten nicht aktualisiert werden",
      monitoringDisabled: "Monitoring deaktiviert",
      hashCopied: "Hash kopiert",
    },
  },
} as const
