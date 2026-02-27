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
  "es-419": {
    trackerStatus: {
      disabled: "Deshabilitado",
      notContacted: "Sin contacto",
      working: "Funcionando",
      updating: "Actualizando",
      error: "Error",
      unknown: "Desconocido",
    },
    magnetHandlerBanner: {
      message: "Registrar qui como manejador de enlaces magnet",
      actions: {
        register: "Registrar",
        dismiss: "Descartar",
      },
      toasts: {
        registrationRequested: "Se solicitó el registro del manejador magnet",
        failedRegister: "No se pudo registrar el manejador magnet",
      },
      guidance: {
        standalone: "Abre qui en una pestaña normal del navegador para registrarlo. Las PWA pueden ocultar el aviso en la barra de direcciones.",
        firefox: "Si el navegador te pide confirmación, acéptala para completar el registro.",
        chromium: "En Chrome suele aparecer un ícono pequeño de manejador de protocolo en la barra de direcciones. Si no aparece, habilítalo en chrome://settings/handlers.",
        default: "Si el navegador te pide confirmación, acéptala para completar el registro.",
      },
    },
    fieldCombobox: {
      title: "Seleccionar campo",
      trigger: {
        selectField: "Seleccionar campo",
      },
      searchPlaceholder: "Buscar campos...",
      empty: "No se encontró ningún campo.",
    },
    dashboardSettingsDialog: {
      actions: {
        layoutSettings: "Configuración de diseño",
      },
      title: "Configuración del dashboard",
      description: "Personaliza qué secciones se muestran y en qué orden.",
      sectionsLabel: "Secciones",
      sectionLabels: {
        serverStats: "Estadísticas del servidor",
        trackerBreakdown: "Desglose por tracker",
        globalStatsCards: "Tarjetas de estadísticas globales",
        instanceCards: "Tarjetas de instancia",
        fallback: "{{sectionId}}",
      },
      trackerDefaultsLabel: "Valores predeterminados del desglose por tracker",
      fields: {
        defaultSort: "Orden predeterminado",
        direction: "Dirección",
        itemsPerPage: "Elementos por página",
      },
      direction: {
        descending: "Descendente",
        ascending: "Ascendente",
      },
      sortColumns: {
        tracker: "Nombre del tracker",
        uploaded: "Subido",
        downloaded: "Descargado",
        ratio: "Ratio",
        buffer: "Buffer",
        count: "Torrents",
        size: "Tamaño",
        performance: "Semeado",
      },
    },
    queryBuilder: {
      emptyState: {
        title: "Sin condiciones",
        description: "Coincide con todos los torrents (respetando la selección de trackers).",
        action: "Agregar condición",
      },
    },
  },
  fr: {
    trackerStatus: {
      disabled: "Désactivé",
      notContacted: "Non contacté",
      working: "Fonctionnel",
      updating: "Mise à jour",
      error: "Erreur",
      unknown: "Inconnu",
    },
    magnetHandlerBanner: {
      message: "Enregistrer qui comme gestionnaire de liens magnet",
      actions: {
        register: "Enregistrer",
        dismiss: "Ignorer",
      },
      toasts: {
        registrationRequested: "Demande d'enregistrement du gestionnaire magnet envoyée",
        failedRegister: "Échec de l'enregistrement du gestionnaire magnet",
      },
      guidance: {
        standalone: "Ouvrez qui dans un onglet navigateur standard pour l'enregistrement. Les PWA masquent parfois l'invite de la barre d'adresse.",
        firefox: "Si le navigateur vous le demande, acceptez pour terminer l'enregistrement.",
        chromium: "Dans Chrome, cela apparaît souvent comme une petite icône de gestionnaire de protocole dans la barre d'adresse. Si rien n'apparaît, activez-le dans chrome://settings/handlers.",
        default: "Si le navigateur vous le demande, acceptez pour terminer l'enregistrement.",
      },
    },
    fieldCombobox: {
      title: "Sélectionner un champ",
      trigger: {
        selectField: "Sélectionner un champ",
      },
      searchPlaceholder: "Rechercher des champs...",
      empty: "Aucun champ trouvé.",
    },
    dashboardSettingsDialog: {
      actions: {
        layoutSettings: "Paramètres de mise en page",
      },
      title: "Paramètres du tableau de bord",
      description: "Personnalisez les sections visibles et leur ordre.",
      sectionsLabel: "Sections",
      sectionLabels: {
        serverStats: "Statistiques serveur",
        trackerBreakdown: "Répartition par tracker",
        globalStatsCards: "Cartes de statistiques globales",
        instanceCards: "Cartes d'instance",
        fallback: "{{sectionId}}",
      },
      trackerDefaultsLabel: "Valeurs par défaut de la répartition tracker",
      fields: {
        defaultSort: "Tri par défaut",
        direction: "Direction",
        itemsPerPage: "Éléments par page",
      },
      direction: {
        descending: "Décroissant",
        ascending: "Croissant",
      },
      sortColumns: {
        tracker: "Nom du tracker",
        uploaded: "Envoyé",
        downloaded: "Téléchargé",
        ratio: "Ratio",
        buffer: "Tampon",
        count: "Torrents",
        size: "Taille",
        performance: "Seedés",
      },
    },
    queryBuilder: {
      emptyState: {
        title: "Aucune condition",
        description: "Correspond à tous les torrents (selon la sélection des trackers).",
        action: "Ajouter une condition",
      },
    },
  },
  ko: {
    trackerStatus: {
      disabled: "비활성화됨",
      notContacted: "접촉 안 됨",
      working: "정상",
      updating: "업데이트 중",
      error: "오류",
      unknown: "알 수 없음",
    },
    magnetHandlerBanner: {
      message: "qui를 magnet 링크 핸들러로 등록",
      actions: {
        register: "등록",
        dismiss: "닫기",
      },
      toasts: {
        registrationRequested: "magnet 핸들러 등록을 요청했습니다",
        failedRegister: "magnet 핸들러 등록에 실패했습니다",
      },
      guidance: {
        standalone: "등록하려면 일반 브라우저 탭에서 qui를 여세요. PWA에서는 주소창 안내가 숨겨질 수 있습니다.",
        firefox: "브라우저에서 확인 요청이 뜨면 승인해 등록을 완료하세요.",
        chromium: "Chrome에서는 주소창의 작은 프로토콜 핸들러 아이콘으로 표시되는 경우가 많습니다. 아무것도 보이지 않으면 chrome://settings/handlers 에서 활성화하세요.",
        default: "브라우저에서 확인 요청이 뜨면 승인해 등록을 완료하세요.",
      },
    },
    fieldCombobox: {
      title: "필드 선택",
      trigger: {
        selectField: "필드 선택",
      },
      searchPlaceholder: "필드 검색...",
      empty: "필드를 찾지 못했습니다.",
    },
    dashboardSettingsDialog: {
      actions: {
        layoutSettings: "레이아웃 설정",
      },
      title: "대시보드 설정",
      description: "보이는 섹션과 순서를 사용자 지정합니다.",
      sectionsLabel: "섹션",
      sectionLabels: {
        serverStats: "서버 통계",
        trackerBreakdown: "트래커 분석",
        globalStatsCards: "전역 통계 카드",
        instanceCards: "인스턴스 카드",
        fallback: "{{sectionId}}",
      },
      trackerDefaultsLabel: "트래커 분석 기본값",
      fields: {
        defaultSort: "기본 정렬",
        direction: "방향",
        itemsPerPage: "페이지당 항목 수",
      },
      direction: {
        descending: "내림차순",
        ascending: "오름차순",
      },
      sortColumns: {
        tracker: "트래커 이름",
        uploaded: "업로드",
        downloaded: "다운로드",
        ratio: "비율",
        buffer: "버퍼",
        count: "토렌트",
        size: "크기",
        performance: "시딩됨",
      },
    },
    queryBuilder: {
      emptyState: {
        title: "조건 없음",
        description: "모든 토렌트와 일치합니다 (트래커 선택은 적용됨).",
        action: "조건 추가",
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
