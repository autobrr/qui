export const sharedUiResources = {
  en: {
    trackerStatus: {
      disabled: "Disabled",
      notContacted: "Not contacted",
      working: "Working",
      updating: "Updating",
      error: "Error",
      unknown: "Unknown",
    },
    magnetHandlerBanner: {
      message: "Register qui as your magnet link handler",
      actions: {
        register: "Register",
        dismiss: "Dismiss",
      },
      toasts: {
        registrationRequested: "Magnet handler registration requested",
        failedRegister: "Failed to register magnet handler",
      },
      guidance: {
        standalone: "Open qui in a regular browser tab to register. The browser prompt may appear in the address bar, which PWAs hide.",
        firefox: "If your browser prompts you, approve it to finish registration.",
        chromium: "In Chrome, this often appears as a small protocol-handler icon in the address bar. If nothing appears, enable handlers at chrome://settings/handlers.",
        default: "If your browser prompts you, approve it to finish registration.",
      },
    },
    fieldCombobox: {
      title: "Select field",
      trigger: {
        selectField: "Select field",
      },
      searchPlaceholder: "Search fields...",
      empty: "No field found.",
    },
    dashboardSettingsDialog: {
      actions: {
        layoutSettings: "Layout Settings",
      },
      title: "Dashboard Settings",
      description: "Customize which sections are visible and their order.",
      sectionsLabel: "Sections",
      sectionLabels: {
        serverStats: "Server Statistics",
        trackerBreakdown: "Tracker Breakdown",
        globalStatsCards: "Global Stats Cards",
        instanceCards: "Instance Cards",
        fallback: "{{sectionId}}",
      },
      trackerDefaultsLabel: "Tracker Breakdown Defaults",
      fields: {
        defaultSort: "Default Sort",
        direction: "Direction",
        itemsPerPage: "Items Per Page",
      },
      direction: {
        descending: "Descending",
        ascending: "Ascending",
      },
      sortColumns: {
        tracker: "Tracker Name",
        uploaded: "Uploaded",
        downloaded: "Downloaded",
        ratio: "Ratio",
        buffer: "Buffer",
        count: "Torrents",
        size: "Size",
        performance: "Seeded",
      },
    },
    queryBuilder: {
      emptyState: {
        title: "No conditions",
        description: "Matches all torrents (subject to tracker selection).",
        action: "Add condition",
      },
    },
  },
  "zh-CN": {
    trackerStatus: {
      disabled: "已禁用",
      notContacted: "未联系",
      working: "正常",
      updating: "更新中",
      error: "错误",
      unknown: "未知",
    },
    magnetHandlerBanner: {
      message: "将 qui 注册为磁力链接处理程序",
      actions: {
        register: "注册",
        dismiss: "忽略",
      },
      toasts: {
        registrationRequested: "已发起磁力处理程序注册请求",
        failedRegister: "注册磁力处理程序失败",
      },
      guidance: {
        standalone: "请在普通浏览器标签页中打开 qui 再注册。PWA 通常不显示地址栏提示。",
        firefox: "如果浏览器弹出确认，请允许以完成注册。",
        chromium: "在 Chrome 中通常会在地址栏显示协议处理图标；如果没有显示，请在 chrome://settings/handlers 中启用。",
        default: "如果浏览器弹出确认，请允许以完成注册。",
      },
    },
    fieldCombobox: {
      title: "选择字段",
      trigger: {
        selectField: "选择字段",
      },
      searchPlaceholder: "搜索字段...",
      empty: "未找到字段。",
    },
    dashboardSettingsDialog: {
      actions: {
        layoutSettings: "布局设置",
      },
      title: "仪表盘设置",
      description: "自定义显示的模块及其顺序。",
      sectionsLabel: "模块",
      sectionLabels: {
        serverStats: "服务器统计",
        trackerBreakdown: "Tracker 统计",
        globalStatsCards: "全局统计卡片",
        instanceCards: "实例卡片",
        fallback: "{{sectionId}}",
      },
      trackerDefaultsLabel: "Tracker 统计默认值",
      fields: {
        defaultSort: "默认排序",
        direction: "方向",
        itemsPerPage: "每页条目数",
      },
      direction: {
        descending: "降序",
        ascending: "升序",
      },
      sortColumns: {
        tracker: "Tracker 名称",
        uploaded: "上传量",
        downloaded: "下载量",
        ratio: "分享率",
        buffer: "缓冲区",
        count: "种子数",
        size: "大小",
        performance: "做种时长",
      },
    },
    queryBuilder: {
      emptyState: {
        title: "暂无条件",
        description: "将匹配所有种子（仍受 Tracker 选择限制）。",
        action: "添加条件",
      },
    },
  },
  ja: {
    trackerStatus: {
      disabled: "無効",
      notContacted: "未接続",
      working: "正常",
      updating: "更新中",
      error: "エラー",
      unknown: "不明",
    },
    magnetHandlerBanner: {
      message: "qui をマグネットリンクのハンドラーとして登録",
      actions: {
        register: "登録",
        dismiss: "閉じる",
      },
      toasts: {
        registrationRequested: "マグネットハンドラーの登録を要求しました",
        failedRegister: "マグネットハンドラーの登録に失敗しました",
      },
      guidance: {
        standalone: "登録するには通常のブラウザタブで qui を開いてください。PWA ではアドレスバーの確認表示が出ない場合があります。",
        firefox: "ブラウザで確認が表示されたら、登録完了のため許可してください。",
        chromium: "Chrome ではアドレスバーの小さなプロトコルハンドラーアイコンに表示されることがあります。表示されない場合は chrome://settings/handlers で有効化してください。",
        default: "ブラウザで確認が表示されたら、登録完了のため許可してください。",
      },
    },
    fieldCombobox: {
      title: "フィールドを選択",
      trigger: {
        selectField: "フィールドを選択",
      },
      searchPlaceholder: "フィールドを検索...",
      empty: "該当するフィールドがありません。",
    },
    dashboardSettingsDialog: {
      actions: {
        layoutSettings: "レイアウト設定",
      },
      title: "ダッシュボード設定",
      description: "表示するセクションと並び順をカスタマイズします。",
      sectionsLabel: "セクション",
      sectionLabels: {
        serverStats: "サーバー統計",
        trackerBreakdown: "トラッカー内訳",
        globalStatsCards: "グローバル統計カード",
        instanceCards: "インスタンスカード",
        fallback: "{{sectionId}}",
      },
      trackerDefaultsLabel: "トラッカー内訳のデフォルト",
      fields: {
        defaultSort: "デフォルトの並び替え",
        direction: "方向",
        itemsPerPage: "1ページの件数",
      },
      direction: {
        descending: "降順",
        ascending: "昇順",
      },
      sortColumns: {
        tracker: "トラッカー名",
        uploaded: "アップロード",
        downloaded: "ダウンロード",
        ratio: "比率",
        buffer: "バッファ",
        count: "トレント数",
        size: "サイズ",
        performance: "シード済み",
      },
    },
    queryBuilder: {
      emptyState: {
        title: "条件がありません",
        description: "すべてのトレントに一致します（トラッカー選択の条件は適用）。",
        action: "条件を追加",
      },
    },
  },
  "pt-BR": {
    trackerStatus: {
      disabled: "Desativado",
      notContacted: "Não contatado",
      working: "Funcionando",
      updating: "Atualizando",
      error: "Erro",
      unknown: "Desconhecido",
    },
    magnetHandlerBanner: {
      message: "Registrar o qui como handler de links magnet",
      actions: {
        register: "Registrar",
        dismiss: "Dispensar",
      },
      toasts: {
        registrationRequested: "Solicitação de registro do handler magnet enviada",
        failedRegister: "Falha ao registrar o handler magnet",
      },
      guidance: {
        standalone: "Abra o qui em uma aba normal do navegador para registrar. PWAs podem ocultar o aviso na barra de endereço.",
        firefox: "Se o navegador solicitar confirmação, aceite para concluir o registro.",
        chromium: "No Chrome, isso costuma aparecer como um ícone pequeno de protocolo na barra de endereço. Se não aparecer, habilite em chrome://settings/handlers.",
        default: "Se o navegador solicitar confirmação, aceite para concluir o registro.",
      },
    },
    fieldCombobox: {
      title: "Selecionar campo",
      trigger: {
        selectField: "Selecionar campo",
      },
      searchPlaceholder: "Buscar campos...",
      empty: "Nenhum campo encontrado.",
    },
    dashboardSettingsDialog: {
      actions: {
        layoutSettings: "Configuração de layout",
      },
      title: "Configurações do dashboard",
      description: "Personalize quais seções ficam visíveis e em que ordem.",
      sectionsLabel: "Seções",
      sectionLabels: {
        serverStats: "Estatísticas do servidor",
        trackerBreakdown: "Resumo por tracker",
        globalStatsCards: "Cards de estatísticas globais",
        instanceCards: "Cards de instância",
        fallback: "{{sectionId}}",
      },
      trackerDefaultsLabel: "Padrões do resumo por tracker",
      fields: {
        defaultSort: "Ordenação padrão",
        direction: "Direção",
        itemsPerPage: "Itens por página",
      },
      direction: {
        descending: "Decrescente",
        ascending: "Crescente",
      },
      sortColumns: {
        tracker: "Nome do tracker",
        uploaded: "Enviado",
        downloaded: "Baixado",
        ratio: "Ratio",
        buffer: "Buffer",
        count: "Torrents",
        size: "Tamanho",
        performance: "Semeado",
      },
    },
    queryBuilder: {
      emptyState: {
        title: "Sem condições",
        description: "Corresponde a todos os torrents (respeitando a seleção de trackers).",
        action: "Adicionar condição",
      },
    },
  },
  de: {
    trackerStatus: {
      disabled: "Deaktiviert",
      notContacted: "Nicht kontaktiert",
      working: "In Ordnung",
      updating: "Aktualisiert",
      error: "Fehler",
      unknown: "Unbekannt",
    },
    magnetHandlerBanner: {
      message: "qui als Handler für Magnet-Links registrieren",
      actions: {
        register: "Registrieren",
        dismiss: "Ausblenden",
      },
      toasts: {
        registrationRequested: "Registrierung des Magnet-Handlers angefordert",
        failedRegister: "Magnet-Handler konnte nicht registriert werden",
      },
      guidance: {
        standalone: "Öffne qui in einem normalen Browser-Tab, um zu registrieren. In PWAs ist die Hinweisleiste in der Adresszeile oft nicht sichtbar.",
        firefox: "Wenn dein Browser nachfragt, bestätige, um die Registrierung abzuschließen.",
        chromium: "In Chrome erscheint dies oft als kleines Protokoll-Handler-Symbol in der Adresszeile. Wenn nichts erscheint, aktiviere Handler unter chrome://settings/handlers.",
        default: "Wenn dein Browser nachfragt, bestätige, um die Registrierung abzuschließen.",
      },
    },
    fieldCombobox: {
      title: "Feld auswählen",
      trigger: {
        selectField: "Feld auswählen",
      },
      searchPlaceholder: "Felder durchsuchen...",
      empty: "Kein Feld gefunden.",
    },
    dashboardSettingsDialog: {
      actions: {
        layoutSettings: "Layout-Einstellungen",
      },
      title: "Dashboard-Einstellungen",
      description: "Lege fest, welche Bereiche sichtbar sind und in welcher Reihenfolge sie erscheinen.",
      sectionsLabel: "Bereiche",
      sectionLabels: {
        serverStats: "Server-Statistiken",
        trackerBreakdown: "Tracker-Aufschlüsselung",
        globalStatsCards: "Globale Statistik-Karten",
        instanceCards: "Instanz-Karten",
        fallback: "{{sectionId}}",
      },
      trackerDefaultsLabel: "Standardwerte für Tracker-Aufschlüsselung",
      fields: {
        defaultSort: "Standard-Sortierung",
        direction: "Richtung",
        itemsPerPage: "Einträge pro Seite",
      },
      direction: {
        descending: "Absteigend",
        ascending: "Aufsteigend",
      },
      sortColumns: {
        tracker: "Tracker-Name",
        uploaded: "Hochgeladen",
        downloaded: "Heruntergeladen",
        ratio: "Verhältnis",
        buffer: "Puffer",
        count: "Torrents",
        size: "Größe",
        performance: "Geseedet",
      },
    },
    queryBuilder: {
      emptyState: {
        title: "Keine Bedingungen",
        description: "Trifft auf alle Torrents zu (abhängig von der Tracker-Auswahl).",
        action: "Bedingung hinzufügen",
      },
    },
  },
} as const
