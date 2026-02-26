export const settingsPageResources = {
  en: {
    header: {
      title: "Settings",
      description: "Manage your application preferences and security",
    },
    tabs: {
      instances: "Instances",
      indexers: "Indexers",
      searchCache: "Search Cache",
      integrations: "Integrations",
      clientProxy: "Client Proxy",
      apiKeys: "API Keys",
      externalPrograms: "External Programs",
      notifications: "Notifications",
      dateTime: "Date & Time",
      premiumThemes: "Premium Themes",
      security: "Security",
      logs: "Logs",
    },
    changePassword: {
      toasts: {
        changed: "Password changed successfully",
        failed: "Failed to change password. Please check your current password.",
      },
      validation: {
        currentRequired: "Current password is required",
        newRequired: "New password is required",
        minLength: "Password must be at least {{count}} characters",
        confirmRequired: "Please confirm your password",
        mismatch: "Passwords do not match",
      },
      labels: {
        currentPassword: "Current Password",
        newPassword: "New Password",
        confirmPassword: "Confirm New Password",
      },
      actions: {
        changing: "Changing...",
        changePassword: "Change Password",
      },
    },
    apiKeys: {
      description: "API keys allow external applications to access your qBittorrent instances.",
      toasts: {
        created: "API key created successfully",
        failedCreate: "Failed to create API key",
        deleted: "API key deleted successfully",
        failedDelete: "Failed to delete API key",
        copied: "API key copied to clipboard",
        failedCopy: "Failed to copy to clipboard",
      },
      actions: {
        create: "Create API Key",
        creating: "Creating...",
        done: "Done",
        cancel: "Cancel",
        delete: "Delete",
      },
      dialogs: {
        createTitle: "Create API Key",
        createDescription: "Give your API key a descriptive name to remember its purpose.",
        yourNewApiKey: "Your new API key",
        saveNowWarning: "Save this key now. You will not be able to see it again.",
        deleteTitle: "Delete API Key?",
        deleteDescription: "This action cannot be undone. Any applications using this key will lose access.",
      },
      form: {
        nameLabel: "Name",
        namePlaceholder: "e.g., Automation Script",
      },
      validation: {
        nameRequired: "Name is required",
      },
      states: {
        loading: "Loading API keys...",
        empty: "No API keys created yet",
      },
      labels: {
        id: "ID: {{id}}",
        created: "Created: {{date}}",
        lastUsed: "Last used: {{date}}",
      },
    },
    instances: {
      toasts: {
        failedReorder: "Failed to update instance order",
      },
      actions: {
        addInstance: "Add Instance",
        addFirstInstance: "Add your first instance",
        adding: "Adding...",
        cancel: "Cancel",
      },
      states: {
        loading: "Loading instances...",
        empty: "No instances configured",
      },
      titleBarSpeeds: {
        label: "Title bar speeds",
        description: "Show download and upload speeds in the browser title bar.",
      },
      dialogs: {
        addTitle: "Add Instance",
        addDescription: "Add a new qBittorrent instance to manage",
      },
    },
    torznabCache: {
      title: "Torznab Search Cache",
      description: "Reduce repeated searches by reusing recent Torznab responses.",
      status: {
        enabled: "Enabled",
        disabled: "Disabled",
      },
      actions: {
        refreshStats: "Refresh stats",
        saveTtl: "Save TTL",
        saving: "Saving...",
      },
      rows: {
        entries: "Entries",
        hitCount: "Hit count",
        approxSize: "Approx. size",
        ttl: "TTL",
        newestEntry: "Newest entry",
        lastUsed: "Last used",
      },
      values: {
        notAvailable: "—",
        minutes: "{{count}} minutes",
      },
      configuration: {
        title: "Configuration",
        description: "Control how long cached searches remain valid.",
        ttlLabel: "Cache TTL (minutes)",
        minimumHelp: "Minimum {{min}} minutes (24 hours). Larger values reduce load on your indexers at the expense of fresher results.",
      },
      toasts: {
        updated: "Cache TTL updated to {{ttl}} minutes",
        failedUpdate: "Failed to update cache TTL",
        enterValidNumber: "Enter a valid number of minutes",
        minimumTtl: "Cache TTL must be at least {{min}} minutes",
      },
    },
    instancesCard: {
      title: "Instances",
      description: "Manage your qBittorrent connection settings",
    },
    integrationsCard: {
      title: "ARR Integrations",
      description: "Configure Sonarr and Radarr instances for enhanced cross-seed searches using external IDs",
    },
    clientApiCard: {
      title: "Client Proxy API Keys",
      description: "Manage API keys for external applications to connect to qBittorrent instances through qui",
    },
    apiCard: {
      title: "API Keys",
      description: "Manage API keys for external access",
      docsTitle: "View API documentation",
      docsText: "API Docs",
    },
    externalProgramsCard: {
      title: "External Programs",
      description: "Configure external programs or scripts that can be executed from the torrent context menu",
    },
    notificationsCard: {
      title: "Notifications",
      description: "Send alerts and status updates via any Shoutrrr-supported service",
    },
    dateTimeCard: {
      title: "Date & Time Preferences",
      description: "Configure timezone, date format, and time display preferences",
    },
    securityCard: {
      changePasswordTitle: "Change Password",
      changePasswordDescription: "Update your account password",
      browserIntegrationTitle: "Browser Integration",
      browserIntegrationDescription: "Configure how your browser handles magnet links",
      browserIntegrationHelp: "Register qui as your browser's handler for magnet links. This lets you open magnet links directly in qui.",
      registerAsHandler: "Register as Handler",
    },
  },
  "zh-CN": {
    header: {
      title: "设置",
      description: "管理应用偏好和安全设置",
    },
    tabs: {
      instances: "实例",
      indexers: "索引器",
      searchCache: "搜索缓存",
      integrations: "集成",
      clientProxy: "客户端代理",
      apiKeys: "API Keys",
      externalPrograms: "外部程序",
      notifications: "通知",
      dateTime: "日期和时间",
      premiumThemes: "高级主题",
      security: "安全",
      logs: "日志",
    },
    changePassword: {
      toasts: {
        changed: "密码修改成功",
        failed: "修改密码失败，请检查当前密码是否正确。",
      },
      validation: {
        currentRequired: "当前密码不能为空",
        newRequired: "新密码不能为空",
        minLength: "密码长度至少为 {{count}} 个字符",
        confirmRequired: "请确认密码",
        mismatch: "两次输入的密码不一致",
      },
      labels: {
        currentPassword: "当前密码",
        newPassword: "新密码",
        confirmPassword: "确认新密码",
      },
      actions: {
        changing: "修改中...",
        changePassword: "修改密码",
      },
    },
    apiKeys: {
      description: "API Key 可供外部应用访问你的 qBittorrent 实例。",
      toasts: {
        created: "API Key 创建成功",
        failedCreate: "创建 API Key 失败",
        deleted: "API Key 删除成功",
        failedDelete: "删除 API Key 失败",
        copied: "API Key 已复制到剪贴板",
        failedCopy: "复制到剪贴板失败",
      },
      actions: {
        create: "创建 API Key",
        creating: "创建中...",
        done: "完成",
        cancel: "取消",
        delete: "删除",
      },
      dialogs: {
        createTitle: "创建 API Key",
        createDescription: "为 API Key 设置一个便于识别用途的名称。",
        yourNewApiKey: "你的新 API Key",
        saveNowWarning: "请立即保存该密钥，之后将无法再次查看。",
        deleteTitle: "删除 API Key？",
        deleteDescription: "此操作无法撤销。使用该密钥的应用将失去访问权限。",
      },
      form: {
        nameLabel: "名称",
        namePlaceholder: "例如：自动化脚本",
      },
      validation: {
        nameRequired: "名称不能为空",
      },
      states: {
        loading: "正在加载 API Keys...",
        empty: "尚未创建 API Keys",
      },
      labels: {
        id: "ID: {{id}}",
        created: "创建时间：{{date}}",
        lastUsed: "最近使用：{{date}}",
      },
    },
    instances: {
      toasts: {
        failedReorder: "更新实例顺序失败",
      },
      actions: {
        addInstance: "添加实例",
        addFirstInstance: "添加你的第一个实例",
        adding: "添加中...",
        cancel: "取消",
      },
      states: {
        loading: "正在加载实例...",
        empty: "尚未配置实例",
      },
      titleBarSpeeds: {
        label: "标题栏速度",
        description: "在浏览器标题栏中显示下载和上传速度。",
      },
      dialogs: {
        addTitle: "添加实例",
        addDescription: "添加一个新的 qBittorrent 实例进行管理",
      },
    },
    torznabCache: {
      title: "Torznab 搜索缓存",
      description: "复用最近的 Torznab 响应以减少重复搜索。",
      status: {
        enabled: "已启用",
        disabled: "已禁用",
      },
      actions: {
        refreshStats: "刷新统计",
        saveTtl: "保存 TTL",
        saving: "保存中...",
      },
      rows: {
        entries: "条目数",
        hitCount: "命中次数",
        approxSize: "预估大小",
        ttl: "TTL",
        newestEntry: "最新条目",
        lastUsed: "最近使用",
      },
      values: {
        notAvailable: "—",
        minutes: "{{count}} 分钟",
      },
      configuration: {
        title: "配置",
        description: "控制缓存搜索结果的有效时长。",
        ttlLabel: "缓存 TTL（分钟）",
        minimumHelp: "最小值为 {{min}} 分钟（24 小时）。值越大，索引器负载越低，但结果新鲜度会下降。",
      },
      toasts: {
        updated: "缓存 TTL 已更新为 {{ttl}} 分钟",
        failedUpdate: "更新缓存 TTL 失败",
        enterValidNumber: "请输入有效的分钟数",
        minimumTtl: "缓存 TTL 不能小于 {{min}} 分钟",
      },
    },
    instancesCard: {
      title: "实例",
      description: "管理 qBittorrent 连接设置",
    },
    integrationsCard: {
      title: "ARR 集成",
      description: "配置 Sonarr 和 Radarr 实例，以便使用外部 ID 提升 cross-seed 搜索效果",
    },
    clientApiCard: {
      title: "客户端代理 API Keys",
      description: "管理供外部应用通过 qui 连接 qBittorrent 实例的 API Keys",
    },
    apiCard: {
      title: "API Keys",
      description: "管理用于外部访问的 API Keys",
      docsTitle: "查看 API 文档",
      docsText: "API 文档",
    },
    externalProgramsCard: {
      title: "外部程序",
      description: "配置可从种子右键菜单执行的外部程序或脚本",
    },
    notificationsCard: {
      title: "通知",
      description: "通过任意支持 Shoutrrr 的服务发送告警和状态更新",
    },
    dateTimeCard: {
      title: "日期和时间偏好",
      description: "配置时区、日期格式和时间显示偏好",
    },
    securityCard: {
      changePasswordTitle: "修改密码",
      changePasswordDescription: "更新你的账户密码",
      browserIntegrationTitle: "浏览器集成",
      browserIntegrationDescription: "配置浏览器处理磁力链接的方式",
      browserIntegrationHelp: "将 qui 注册为浏览器的磁力链接处理器，这样可直接在 qui 中打开磁力链接。",
      registerAsHandler: "注册为处理器",
    },
  },
  ja: {
    header: {
      title: "設定",
      description: "アプリの設定とセキュリティを管理します",
    },
    tabs: {
      instances: "インスタンス",
      indexers: "インデクサー",
      searchCache: "検索キャッシュ",
      integrations: "連携",
      clientProxy: "クライアントプロキシ",
      apiKeys: "API キー",
      externalPrograms: "外部プログラム",
      notifications: "通知",
      dateTime: "日時",
      premiumThemes: "プレミアムテーマ",
      security: "セキュリティ",
      logs: "ログ",
    },
    changePassword: {
      toasts: {
        changed: "パスワードを変更しました",
        failed: "パスワードの変更に失敗しました。現在のパスワードを確認してください。",
      },
      validation: {
        currentRequired: "現在のパスワードは必須です",
        newRequired: "新しいパスワードは必須です",
        minLength: "パスワードは {{count}} 文字以上で入力してください",
        confirmRequired: "パスワード確認を入力してください",
        mismatch: "パスワードが一致しません",
      },
      labels: {
        currentPassword: "現在のパスワード",
        newPassword: "新しいパスワード",
        confirmPassword: "新しいパスワード（確認）",
      },
      actions: {
        changing: "変更中...",
        changePassword: "パスワードを変更",
      },
    },
    apiKeys: {
      description: "API キーを使うと、外部アプリから qBittorrent インスタンスにアクセスできます。",
      toasts: {
        created: "API キーを作成しました",
        failedCreate: "API キーの作成に失敗しました",
        deleted: "API キーを削除しました",
        failedDelete: "API キーの削除に失敗しました",
        copied: "API キーをクリップボードにコピーしました",
        failedCopy: "クリップボードへのコピーに失敗しました",
      },
      actions: {
        create: "API キーを作成",
        creating: "作成中...",
        done: "完了",
        cancel: "キャンセル",
        delete: "削除",
      },
      dialogs: {
        createTitle: "API キーを作成",
        createDescription: "用途が分かる説明的な名前を付けてください。",
        yourNewApiKey: "新しい API キー",
        saveNowWarning: "このキーは今すぐ保存してください。後から再表示できません。",
        deleteTitle: "API キーを削除しますか？",
        deleteDescription: "この操作は取り消せません。このキーを使うアプリはアクセスできなくなります。",
      },
      form: {
        nameLabel: "名前",
        namePlaceholder: "例: Automation Script",
      },
      validation: {
        nameRequired: "名前は必須です",
      },
      states: {
        loading: "API キーを読み込み中...",
        empty: "API キーはまだ作成されていません",
      },
      labels: {
        id: "ID: {{id}}",
        created: "作成: {{date}}",
        lastUsed: "最終使用: {{date}}",
      },
    },
    instances: {
      toasts: {
        failedReorder: "インスタンスの並び替え更新に失敗しました",
      },
      actions: {
        addInstance: "インスタンスを追加",
        addFirstInstance: "最初のインスタンスを追加",
        adding: "追加中...",
        cancel: "キャンセル",
      },
      states: {
        loading: "インスタンスを読み込み中...",
        empty: "インスタンスが設定されていません",
      },
      titleBarSpeeds: {
        label: "タイトルバー速度表示",
        description: "ブラウザのタイトルバーにダウンロード・アップロード速度を表示します。",
      },
      dialogs: {
        addTitle: "インスタンスを追加",
        addDescription: "管理する qBittorrent インスタンスを追加します",
      },
    },
    torznabCache: {
      title: "Torznab 検索キャッシュ",
      description: "最近の Torznab 応答を再利用して重複検索を減らします。",
      status: {
        enabled: "有効",
        disabled: "無効",
      },
      actions: {
        refreshStats: "統計を更新",
        saveTtl: "TTL を保存",
        saving: "保存中...",
      },
      rows: {
        entries: "エントリ数",
        hitCount: "ヒット数",
        approxSize: "概算サイズ",
        ttl: "TTL",
        newestEntry: "最新エントリ",
        lastUsed: "最終使用",
      },
      values: {
        notAvailable: "—",
        minutes: "{{count}} 分",
      },
      configuration: {
        title: "設定",
        description: "キャッシュ検索結果の有効期間を設定します。",
        ttlLabel: "キャッシュ TTL（分）",
        minimumHelp: "最小は {{min}} 分（24 時間）です。値を大きくするとインデクサー負荷は下がりますが、結果の鮮度は下がります。",
      },
      toasts: {
        updated: "キャッシュ TTL を {{ttl}} 分に更新しました",
        failedUpdate: "キャッシュ TTL の更新に失敗しました",
        enterValidNumber: "有効な分数を入力してください",
        minimumTtl: "キャッシュ TTL は最低 {{min}} 分にしてください",
      },
    },
    instancesCard: {
      title: "インスタンス",
      description: "qBittorrent 接続設定を管理します",
    },
    integrationsCard: {
      title: "ARR 連携",
      description: "Sonarr と Radarr のインスタンスを設定し、外部 ID を使った cross-seed 検索を強化します",
    },
    clientApiCard: {
      title: "クライアントプロキシ API キー",
      description: "外部アプリが qui 経由で qBittorrent インスタンスに接続するための API キーを管理します",
    },
    apiCard: {
      title: "API キー",
      description: "外部アクセス用 API キーを管理します",
      docsTitle: "API ドキュメントを表示",
      docsText: "API ドキュメント",
    },
    externalProgramsCard: {
      title: "外部プログラム",
      description: "Torrent のコンテキストメニューから実行できる外部プログラムやスクリプトを設定します",
    },
    notificationsCard: {
      title: "通知",
      description: "Shoutrrr 対応サービスでアラートやステータス更新を送信します",
    },
    dateTimeCard: {
      title: "日時設定",
      description: "タイムゾーン、日付形式、時刻表示を設定します",
    },
    securityCard: {
      changePasswordTitle: "パスワード変更",
      changePasswordDescription: "アカウントのパスワードを更新します",
      browserIntegrationTitle: "ブラウザ連携",
      browserIntegrationDescription: "ブラウザでのマグネットリンク処理を設定します",
      browserIntegrationHelp: "qui をブラウザのマグネットリンクハンドラーとして登録すると、マグネットリンクを直接 qui で開けます。",
      registerAsHandler: "ハンドラーとして登録",
    },
  },
  "pt-BR": {
    header: {
      title: "Configurações",
      description: "Gerencie preferências e segurança do aplicativo",
    },
    tabs: {
      instances: "Instâncias",
      indexers: "Indexadores",
      searchCache: "Cache de busca",
      integrations: "Integrações",
      clientProxy: "Proxy do cliente",
      apiKeys: "Chaves de API",
      externalPrograms: "Programas externos",
      notifications: "Notificações",
      dateTime: "Data e hora",
      premiumThemes: "Temas premium",
      security: "Segurança",
      logs: "Logs",
    },
    changePassword: {
      toasts: {
        changed: "Senha alterada com sucesso",
        failed: "Falha ao alterar senha. Verifique sua senha atual.",
      },
      validation: {
        currentRequired: "A senha atual é obrigatória",
        newRequired: "A nova senha é obrigatória",
        minLength: "A senha deve ter pelo menos {{count}} caracteres",
        confirmRequired: "Confirme sua senha",
        mismatch: "As senhas não coincidem",
      },
      labels: {
        currentPassword: "Senha atual",
        newPassword: "Nova senha",
        confirmPassword: "Confirmar nova senha",
      },
      actions: {
        changing: "Alterando...",
        changePassword: "Alterar senha",
      },
    },
    apiKeys: {
      description: "As chaves de API permitem que aplicativos externos acessem suas instâncias do qBittorrent.",
      toasts: {
        created: "Chave de API criada com sucesso",
        failedCreate: "Falha ao criar chave de API",
        deleted: "Chave de API removida com sucesso",
        failedDelete: "Falha ao remover chave de API",
        copied: "Chave de API copiada para a área de transferência",
        failedCopy: "Falha ao copiar para a área de transferência",
      },
      actions: {
        create: "Criar chave de API",
        creating: "Criando...",
        done: "Concluir",
        cancel: "Cancelar",
        delete: "Excluir",
      },
      dialogs: {
        createTitle: "Criar chave de API",
        createDescription: "Dê um nome descritivo para lembrar a finalidade da chave.",
        yourNewApiKey: "Sua nova chave de API",
        saveNowWarning: "Salve esta chave agora. Você não poderá vê-la novamente.",
        deleteTitle: "Excluir chave de API?",
        deleteDescription: "Esta ação não pode ser desfeita. Aplicativos que usam esta chave perderão acesso.",
      },
      form: {
        nameLabel: "Nome",
        namePlaceholder: "ex.: Script de automação",
      },
      validation: {
        nameRequired: "Nome é obrigatório",
      },
      states: {
        loading: "Carregando chaves de API...",
        empty: "Nenhuma chave de API criada ainda",
      },
      labels: {
        id: "ID: {{id}}",
        created: "Criada em: {{date}}",
        lastUsed: "Último uso: {{date}}",
      },
    },
    instances: {
      toasts: {
        failedReorder: "Falha ao atualizar a ordem das instâncias",
      },
      actions: {
        addInstance: "Adicionar instância",
        addFirstInstance: "Adicionar sua primeira instância",
        adding: "Adicionando...",
        cancel: "Cancelar",
      },
      states: {
        loading: "Carregando instâncias...",
        empty: "Nenhuma instância configurada",
      },
      titleBarSpeeds: {
        label: "Velocidades na barra de título",
        description: "Mostra as velocidades de download e upload na barra de título do navegador.",
      },
      dialogs: {
        addTitle: "Adicionar instância",
        addDescription: "Adicione uma nova instância do qBittorrent para gerenciar",
      },
    },
    torznabCache: {
      title: "Cache de busca Torznab",
      description: "Reduza buscas repetidas reutilizando respostas recentes do Torznab.",
      status: {
        enabled: "Ativado",
        disabled: "Desativado",
      },
      actions: {
        refreshStats: "Atualizar estatísticas",
        saveTtl: "Salvar TTL",
        saving: "Salvando...",
      },
      rows: {
        entries: "Entradas",
        hitCount: "Total de acertos",
        approxSize: "Tamanho aprox.",
        ttl: "TTL",
        newestEntry: "Entrada mais recente",
        lastUsed: "Último uso",
      },
      values: {
        notAvailable: "—",
        minutes: "{{count}} minutos",
      },
      configuration: {
        title: "Configuração",
        description: "Controle por quanto tempo as buscas em cache permanecem válidas.",
        ttlLabel: "TTL do cache (minutos)",
        minimumHelp: "Mínimo de {{min}} minutos (24 horas). Valores maiores reduzem carga nos indexadores, com menor frescor dos resultados.",
      },
      toasts: {
        updated: "TTL do cache atualizado para {{ttl}} minutos",
        failedUpdate: "Falha ao atualizar TTL do cache",
        enterValidNumber: "Digite um número de minutos válido",
        minimumTtl: "O TTL do cache deve ser de pelo menos {{min}} minutos",
      },
    },
    instancesCard: {
      title: "Instâncias",
      description: "Gerencie configurações de conexão do qBittorrent",
    },
    integrationsCard: {
      title: "Integrações ARR",
      description: "Configure instâncias do Sonarr e Radarr para melhorar buscas de cross-seed com IDs externos",
    },
    clientApiCard: {
      title: "Chaves de API de proxy do cliente",
      description: "Gerencie chaves de API para aplicativos externos conectarem às instâncias do qBittorrent via qui",
    },
    apiCard: {
      title: "Chaves de API",
      description: "Gerencie chaves de API para acesso externo",
      docsTitle: "Ver documentação da API",
      docsText: "Docs da API",
    },
    externalProgramsCard: {
      title: "Programas externos",
      description: "Configure programas externos ou scripts executáveis pelo menu de contexto do torrent",
    },
    notificationsCard: {
      title: "Notificações",
      description: "Envie alertas e atualizações de status por qualquer serviço compatível com Shoutrrr",
    },
    dateTimeCard: {
      title: "Preferências de data e hora",
      description: "Configure fuso horário, formato de data e preferências de exibição de hora",
    },
    securityCard: {
      changePasswordTitle: "Alterar senha",
      changePasswordDescription: "Atualize a senha da sua conta",
      browserIntegrationTitle: "Integração com navegador",
      browserIntegrationDescription: "Configure como seu navegador lida com links magnet",
      browserIntegrationHelp: "Registre o qui como manipulador de links magnet do navegador. Assim você abre links magnet direto no qui.",
      registerAsHandler: "Registrar como manipulador",
    },
  },
  de: {
    header: {
      title: "Einstellungen",
      description: "Verwalte Anwendungspräferenzen und Sicherheit",
    },
    tabs: {
      instances: "Instanzen",
      indexers: "Indexer",
      searchCache: "Such-Cache",
      integrations: "Integrationen",
      clientProxy: "Client-Proxy",
      apiKeys: "API-Schlüssel",
      externalPrograms: "Externe Programme",
      notifications: "Benachrichtigungen",
      dateTime: "Datum und Uhrzeit",
      premiumThemes: "Premium-Themes",
      security: "Sicherheit",
      logs: "Logs",
    },
    changePassword: {
      toasts: {
        changed: "Passwort erfolgreich geändert",
        failed: "Passwort konnte nicht geändert werden. Bitte prüfe dein aktuelles Passwort.",
      },
      validation: {
        currentRequired: "Aktuelles Passwort ist erforderlich",
        newRequired: "Neues Passwort ist erforderlich",
        minLength: "Das Passwort muss mindestens {{count}} Zeichen lang sein",
        confirmRequired: "Bitte bestätige dein Passwort",
        mismatch: "Passwörter stimmen nicht überein",
      },
      labels: {
        currentPassword: "Aktuelles Passwort",
        newPassword: "Neues Passwort",
        confirmPassword: "Neues Passwort bestätigen",
      },
      actions: {
        changing: "Wird geändert...",
        changePassword: "Passwort ändern",
      },
    },
    apiKeys: {
      description: "API-Schlüssel erlauben externen Anwendungen den Zugriff auf deine qBittorrent-Instanzen.",
      toasts: {
        created: "API-Schlüssel erfolgreich erstellt",
        failedCreate: "API-Schlüssel konnte nicht erstellt werden",
        deleted: "API-Schlüssel erfolgreich gelöscht",
        failedDelete: "API-Schlüssel konnte nicht gelöscht werden",
        copied: "API-Schlüssel in die Zwischenablage kopiert",
        failedCopy: "Kopieren in die Zwischenablage fehlgeschlagen",
      },
      actions: {
        create: "API-Schlüssel erstellen",
        creating: "Wird erstellt...",
        done: "Fertig",
        cancel: "Abbrechen",
        delete: "Löschen",
      },
      dialogs: {
        createTitle: "API-Schlüssel erstellen",
        createDescription: "Gib dem API-Schlüssel einen eindeutigen Namen zur späteren Zuordnung.",
        yourNewApiKey: "Dein neuer API-Schlüssel",
        saveNowWarning: "Speichere diesen Schlüssel jetzt. Er wird später nicht erneut angezeigt.",
        deleteTitle: "API-Schlüssel löschen?",
        deleteDescription: "Diese Aktion kann nicht rückgängig gemacht werden. Anwendungen mit diesem Schlüssel verlieren den Zugriff.",
      },
      form: {
        nameLabel: "Name",
        namePlaceholder: "z. B. Automationsskript",
      },
      validation: {
        nameRequired: "Name ist erforderlich",
      },
      states: {
        loading: "API-Schlüssel werden geladen...",
        empty: "Noch keine API-Schlüssel erstellt",
      },
      labels: {
        id: "ID: {{id}}",
        created: "Erstellt: {{date}}",
        lastUsed: "Zuletzt verwendet: {{date}}",
      },
    },
    instances: {
      toasts: {
        failedReorder: "Instanzreihenfolge konnte nicht aktualisiert werden",
      },
      actions: {
        addInstance: "Instanz hinzufügen",
        addFirstInstance: "Erste Instanz hinzufügen",
        adding: "Wird hinzugefügt...",
        cancel: "Abbrechen",
      },
      states: {
        loading: "Instanzen werden geladen...",
        empty: "Keine Instanzen konfiguriert",
      },
      titleBarSpeeds: {
        label: "Geschwindigkeit in der Titelleiste",
        description: "Zeigt Download- und Upload-Geschwindigkeiten in der Browser-Titelleiste an.",
      },
      dialogs: {
        addTitle: "Instanz hinzufügen",
        addDescription: "Füge eine neue qBittorrent-Instanz zur Verwaltung hinzu",
      },
    },
    torznabCache: {
      title: "Torznab-Such-Cache",
      description: "Reduziere wiederholte Suchen durch Wiederverwendung aktueller Torznab-Antworten.",
      status: {
        enabled: "Aktiviert",
        disabled: "Deaktiviert",
      },
      actions: {
        refreshStats: "Statistik aktualisieren",
        saveTtl: "TTL speichern",
        saving: "Speichern...",
      },
      rows: {
        entries: "Einträge",
        hitCount: "Trefferanzahl",
        approxSize: "Ungefähre Größe",
        ttl: "TTL",
        newestEntry: "Neuester Eintrag",
        lastUsed: "Zuletzt verwendet",
      },
      values: {
        notAvailable: "—",
        minutes: "{{count}} Minuten",
      },
      configuration: {
        title: "Konfiguration",
        description: "Lege fest, wie lange zwischengespeicherte Suchergebnisse gültig bleiben.",
        ttlLabel: "Cache-TTL (Minuten)",
        minimumHelp: "Minimum {{min}} Minuten (24 Stunden). Höhere Werte verringern die Last auf den Indexern, liefern aber weniger aktuelle Ergebnisse.",
      },
      toasts: {
        updated: "Cache-TTL auf {{ttl}} Minuten aktualisiert",
        failedUpdate: "Cache-TTL konnte nicht aktualisiert werden",
        enterValidNumber: "Bitte gib eine gültige Minutenanzahl ein",
        minimumTtl: "Cache-TTL muss mindestens {{min}} Minuten betragen",
      },
    },
    instancesCard: {
      title: "Instanzen",
      description: "Verwalte deine qBittorrent-Verbindungseinstellungen",
    },
    integrationsCard: {
      title: "ARR-Integrationen",
      description: "Konfiguriere Sonarr- und Radarr-Instanzen für bessere Cross-Seed-Suchen mit externen IDs",
    },
    clientApiCard: {
      title: "Client-Proxy-API-Schlüssel",
      description: "Verwalte API-Schlüssel für externe Anwendungen, die sich über qui mit qBittorrent-Instanzen verbinden",
    },
    apiCard: {
      title: "API-Schlüssel",
      description: "Verwalte API-Schlüssel für externen Zugriff",
      docsTitle: "API-Dokumentation anzeigen",
      docsText: "API-Doku",
    },
    externalProgramsCard: {
      title: "Externe Programme",
      description: "Konfiguriere externe Programme oder Skripte, die aus dem Torrent-Kontextmenü ausgeführt werden können",
    },
    notificationsCard: {
      title: "Benachrichtigungen",
      description: "Sende Warnungen und Statusupdates über jeden Shoutrrr-kompatiblen Dienst",
    },
    dateTimeCard: {
      title: "Datum- und Uhrzeit-Einstellungen",
      description: "Konfiguriere Zeitzone, Datumsformat und Zeitanzeige",
    },
    securityCard: {
      changePasswordTitle: "Passwort ändern",
      changePasswordDescription: "Aktualisiere dein Kontopasswort",
      browserIntegrationTitle: "Browser-Integration",
      browserIntegrationDescription: "Lege fest, wie dein Browser Magnet-Links verarbeitet",
      browserIntegrationHelp: "Registriere qui als Magnet-Link-Handler deines Browsers. So kannst du Magnet-Links direkt in qui öffnen.",
      registerAsHandler: "Als Handler registrieren",
    },
  },
} as const
