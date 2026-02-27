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
  "es-419": {
    header: {
      title: "Configuración",
      description: "Administra tus preferencias y la seguridad de la aplicación",
    },
    tabs: {
      instances: "Instancias",
      indexers: "Indexadores",
      searchCache: "Caché de búsqueda",
      integrations: "Integraciones",
      clientProxy: "Proxy del cliente",
      apiKeys: "API Keys",
      externalPrograms: "Programas externos",
      notifications: "Notificaciones",
      dateTime: "Fecha y hora",
      premiumThemes: "Temas premium",
      security: "Seguridad",
      logs: "Registros",
    },
    changePassword: {
      toasts: {
        changed: "Contraseña cambiada correctamente",
        failed: "No se pudo cambiar la contraseña. Verifica tu contraseña actual.",
      },
      validation: {
        currentRequired: "La contraseña actual es obligatoria",
        newRequired: "La nueva contraseña es obligatoria",
        minLength: "La contraseña debe tener al menos {{count}} caracteres",
        confirmRequired: "Confirma tu contraseña",
        mismatch: "Las contraseñas no coinciden",
      },
      labels: {
        currentPassword: "Contraseña actual",
        newPassword: "Nueva contraseña",
        confirmPassword: "Confirmar nueva contraseña",
      },
      actions: {
        changing: "Cambiando...",
        changePassword: "Cambiar contraseña",
      },
    },
    apiKeys: {
      description: "Las API keys permiten que aplicaciones externas accedan a tus instancias de qBittorrent.",
      toasts: {
        created: "API key creada correctamente",
        failedCreate: "No se pudo crear la API key",
        deleted: "API key eliminada correctamente",
        failedDelete: "No se pudo eliminar la API key",
        copied: "API key copiada al portapapeles",
        failedCopy: "No se pudo copiar al portapapeles",
      },
      actions: {
        create: "Crear API key",
        creating: "Creando...",
        done: "Listo",
        cancel: "Cancelar",
        delete: "Eliminar",
      },
      dialogs: {
        createTitle: "Crear API key",
        createDescription: "Dale un nombre descriptivo para recordar su propósito.",
        yourNewApiKey: "Tu nueva API key",
        saveNowWarning: "Guarda esta clave ahora. No podrás verla de nuevo.",
        deleteTitle: "¿Eliminar API key?",
        deleteDescription: "Esta acción no se puede deshacer. Las aplicaciones que usen esta clave perderán acceso.",
      },
      form: {
        nameLabel: "Nombre",
        namePlaceholder: "p. ej., Script de automatización",
      },
      validation: {
        nameRequired: "El nombre es obligatorio",
      },
      states: {
        loading: "Cargando API keys...",
        empty: "Aún no se crearon API keys",
      },
      labels: {
        id: "ID: {{id}}",
        created: "Creada: {{date}}",
        lastUsed: "Último uso: {{date}}",
      },
    },
    instances: {
      toasts: {
        failedReorder: "No se pudo actualizar el orden de instancias",
      },
      actions: {
        addInstance: "Agregar instancia",
        addFirstInstance: "Agregar tu primera instancia",
        adding: "Agregando...",
        cancel: "Cancelar",
      },
      states: {
        loading: "Cargando instancias...",
        empty: "No hay instancias configuradas",
      },
      titleBarSpeeds: {
        label: "Velocidades en la barra de título",
        description: "Muestra velocidades de descarga y subida en la barra de título del navegador.",
      },
      dialogs: {
        addTitle: "Agregar instancia",
        addDescription: "Agrega una nueva instancia de qBittorrent para administrar",
      },
    },
    torznabCache: {
      title: "Caché de búsqueda Torznab",
      description: "Reduce búsquedas repetidas reutilizando respuestas recientes de Torznab.",
      status: {
        enabled: "Habilitado",
        disabled: "Deshabilitado",
      },
      actions: {
        refreshStats: "Actualizar estadísticas",
        saveTtl: "Guardar TTL",
        saving: "Guardando...",
      },
      rows: {
        entries: "Entradas",
        hitCount: "Conteo de aciertos",
        approxSize: "Tamaño aprox.",
        ttl: "TTL",
        newestEntry: "Entrada más reciente",
        lastUsed: "Último uso",
      },
      values: {
        notAvailable: "—",
        minutes: "{{count}} minutos",
      },
      configuration: {
        title: "Configuración",
        description: "Controla cuánto tiempo siguen siendo válidas las búsquedas en caché.",
        ttlLabel: "TTL de caché (minutos)",
        minimumHelp: "Mínimo {{min}} minutos (24 horas). Valores más altos reducen la carga en tus indexadores, a costa de resultados menos frescos.",
      },
      toasts: {
        updated: "TTL de caché actualizado a {{ttl}} minutos",
        failedUpdate: "No se pudo actualizar el TTL de caché",
        enterValidNumber: "Ingresa un número válido de minutos",
        minimumTtl: "El TTL de caché debe ser de al menos {{min}} minutos",
      },
    },
    instancesCard: {
      title: "Instancias",
      description: "Administra la configuración de conexión de qBittorrent",
    },
    integrationsCard: {
      title: "Integraciones ARR",
      description: "Configura instancias de Sonarr y Radarr para mejorar búsquedas cross-seed con IDs externos",
    },
    clientApiCard: {
      title: "API keys de proxy del cliente",
      description: "Administra API keys para que aplicaciones externas se conecten a instancias de qBittorrent mediante qui",
    },
    apiCard: {
      title: "API Keys",
      description: "Administra API keys para acceso externo",
      docsTitle: "Ver documentación de API",
      docsText: "Docs de API",
    },
    externalProgramsCard: {
      title: "Programas externos",
      description: "Configura programas externos o scripts ejecutables desde el menú contextual del torrent",
    },
    notificationsCard: {
      title: "Notificaciones",
      description: "Envía alertas y actualizaciones de estado mediante cualquier servicio compatible con Shoutrrr",
    },
    dateTimeCard: {
      title: "Preferencias de fecha y hora",
      description: "Configura zona horaria, formato de fecha y preferencias de visualización de hora",
    },
    securityCard: {
      changePasswordTitle: "Cambiar contraseña",
      changePasswordDescription: "Actualiza la contraseña de tu cuenta",
      browserIntegrationTitle: "Integración con navegador",
      browserIntegrationDescription: "Configura cómo tu navegador maneja enlaces magnet",
      browserIntegrationHelp: "Registra qui como manejador de enlaces magnet en tu navegador. Así puedes abrir enlaces magnet directamente en qui.",
      registerAsHandler: "Registrar como manejador",
    },
  },
  fr: {
    header: {
      title: "Paramètres",
      description: "Gérez vos préférences d'application et la sécurité",
    },
    tabs: {
      instances: "Instances",
      indexers: "Indexeurs",
      searchCache: "Cache de recherche",
      integrations: "Intégrations",
      clientProxy: "Proxy client",
      apiKeys: "Clés API",
      externalPrograms: "Programmes externes",
      notifications: "Notifications",
      dateTime: "Date et heure",
      premiumThemes: "Thèmes premium",
      security: "Sécurité",
      logs: "Journaux",
    },
    changePassword: {
      toasts: {
        changed: "Mot de passe modifié avec succès",
        failed: "Échec du changement de mot de passe. Vérifiez votre mot de passe actuel.",
      },
      validation: {
        currentRequired: "Le mot de passe actuel est requis",
        newRequired: "Le nouveau mot de passe est requis",
        minLength: "Le mot de passe doit comporter au moins {{count}} caractères",
        confirmRequired: "Veuillez confirmer votre mot de passe",
        mismatch: "Les mots de passe ne correspondent pas",
      },
      labels: {
        currentPassword: "Mot de passe actuel",
        newPassword: "Nouveau mot de passe",
        confirmPassword: "Confirmer le nouveau mot de passe",
      },
      actions: {
        changing: "Modification...",
        changePassword: "Changer le mot de passe",
      },
    },
    apiKeys: {
      description: "Les clés API permettent aux applications externes d'accéder à vos instances qBittorrent.",
      toasts: {
        created: "Clé API créée avec succès",
        failedCreate: "Échec de la création de la clé API",
        deleted: "Clé API supprimée avec succès",
        failedDelete: "Échec de la suppression de la clé API",
        copied: "Clé API copiée dans le presse-papiers",
        failedCopy: "Échec de la copie dans le presse-papiers",
      },
      actions: {
        create: "Créer une clé API",
        creating: "Création...",
        done: "Terminé",
        cancel: "Annuler",
        delete: "Supprimer",
      },
      dialogs: {
        createTitle: "Créer une clé API",
        createDescription: "Donnez un nom descriptif à votre clé API pour en retenir l'usage.",
        yourNewApiKey: "Votre nouvelle clé API",
        saveNowWarning: "Enregistrez cette clé maintenant. Vous ne pourrez plus la voir ensuite.",
        deleteTitle: "Supprimer la clé API ?",
        deleteDescription: "Cette action est irréversible. Les applications qui utilisent cette clé perdront l'accès.",
      },
      form: {
        nameLabel: "Nom",
        namePlaceholder: "ex. : Script d'automatisation",
      },
      validation: {
        nameRequired: "Le nom est requis",
      },
      states: {
        loading: "Chargement des clés API...",
        empty: "Aucune clé API créée pour le moment",
      },
      labels: {
        id: "ID : {{id}}",
        created: "Créée : {{date}}",
        lastUsed: "Dernière utilisation : {{date}}",
      },
    },
    instances: {
      toasts: {
        failedReorder: "Échec de la mise à jour de l'ordre des instances",
      },
      actions: {
        addInstance: "Ajouter une instance",
        addFirstInstance: "Ajouter votre première instance",
        adding: "Ajout...",
        cancel: "Annuler",
      },
      states: {
        loading: "Chargement des instances...",
        empty: "Aucune instance configurée",
      },
      titleBarSpeeds: {
        label: "Vitesses dans la barre de titre",
        description: "Affiche les vitesses de téléchargement et d'envoi dans la barre de titre du navigateur.",
      },
      dialogs: {
        addTitle: "Ajouter une instance",
        addDescription: "Ajoutez une nouvelle instance qBittorrent à gérer",
      },
    },
    torznabCache: {
      title: "Cache de recherche Torznab",
      description: "Réduisez les recherches répétées en réutilisant des réponses Torznab récentes.",
      status: {
        enabled: "Activé",
        disabled: "Désactivé",
      },
      actions: {
        refreshStats: "Actualiser les stats",
        saveTtl: "Enregistrer le TTL",
        saving: "Enregistrement...",
      },
      rows: {
        entries: "Entrées",
        hitCount: "Nombre de hits",
        approxSize: "Taille approx.",
        ttl: "TTL",
        newestEntry: "Entrée la plus récente",
        lastUsed: "Dernière utilisation",
      },
      values: {
        notAvailable: "—",
        minutes: "{{count}} minutes",
      },
      configuration: {
        title: "Configuration",
        description: "Contrôlez combien de temps les recherches en cache restent valides.",
        ttlLabel: "TTL du cache (minutes)",
        minimumHelp: "Minimum {{min}} minutes (24 heures). Des valeurs plus élevées réduisent la charge sur vos indexeurs au détriment de la fraîcheur des résultats.",
      },
      toasts: {
        updated: "TTL du cache mis à jour à {{ttl}} minutes",
        failedUpdate: "Échec de la mise à jour du TTL du cache",
        enterValidNumber: "Entrez un nombre valide de minutes",
        minimumTtl: "Le TTL du cache doit être d'au moins {{min}} minutes",
      },
    },
    instancesCard: {
      title: "Instances",
      description: "Gérez vos paramètres de connexion qBittorrent",
    },
    integrationsCard: {
      title: "Intégrations ARR",
      description: "Configurez des instances Sonarr et Radarr pour améliorer les recherches cross-seed avec des IDs externes",
    },
    clientApiCard: {
      title: "Clés API du proxy client",
      description: "Gérez les clés API pour que des applications externes se connectent aux instances qBittorrent via qui",
    },
    apiCard: {
      title: "Clés API",
      description: "Gérez les clés API pour l'accès externe",
      docsTitle: "Voir la documentation de l'API",
      docsText: "Docs API",
    },
    externalProgramsCard: {
      title: "Programmes externes",
      description: "Configurez des programmes externes ou scripts exécutables depuis le menu contextuel des torrents",
    },
    notificationsCard: {
      title: "Notifications",
      description: "Envoyez des alertes et mises à jour d'état via n'importe quel service compatible Shoutrrr",
    },
    dateTimeCard: {
      title: "Préférences date et heure",
      description: "Configurez le fuseau horaire, le format de date et les préférences d'affichage de l'heure",
    },
    securityCard: {
      changePasswordTitle: "Changer le mot de passe",
      changePasswordDescription: "Mettez à jour le mot de passe de votre compte",
      browserIntegrationTitle: "Intégration navigateur",
      browserIntegrationDescription: "Configurez la manière dont votre navigateur gère les liens magnet",
      browserIntegrationHelp: "Enregistrez qui comme gestionnaire de liens magnet dans votre navigateur. Vous pourrez ainsi ouvrir directement les liens magnet dans qui.",
      registerAsHandler: "Enregistrer comme gestionnaire",
    },
  },
  ko: {
    header: {
      title: "설정",
      description: "앱 환경설정과 보안을 관리합니다",
    },
    tabs: {
      instances: "인스턴스",
      indexers: "인덱서",
      searchCache: "검색 캐시",
      integrations: "통합",
      clientProxy: "클라이언트 프록시",
      apiKeys: "API 키",
      externalPrograms: "외부 프로그램",
      notifications: "알림",
      dateTime: "날짜 및 시간",
      premiumThemes: "프리미엄 테마",
      security: "보안",
      logs: "로그",
    },
    changePassword: {
      toasts: {
        changed: "비밀번호를 변경했습니다",
        failed: "비밀번호 변경에 실패했습니다. 현재 비밀번호를 확인하세요.",
      },
      validation: {
        currentRequired: "현재 비밀번호는 필수입니다",
        newRequired: "새 비밀번호는 필수입니다",
        minLength: "비밀번호는 최소 {{count}}자 이상이어야 합니다",
        confirmRequired: "비밀번호를 확인해 주세요",
        mismatch: "비밀번호가 일치하지 않습니다",
      },
      labels: {
        currentPassword: "현재 비밀번호",
        newPassword: "새 비밀번호",
        confirmPassword: "새 비밀번호 확인",
      },
      actions: {
        changing: "변경 중...",
        changePassword: "비밀번호 변경",
      },
    },
    apiKeys: {
      description: "API 키를 사용하면 외부 애플리케이션이 qBittorrent 인스턴스에 접근할 수 있습니다.",
      toasts: {
        created: "API 키를 생성했습니다",
        failedCreate: "API 키 생성에 실패했습니다",
        deleted: "API 키를 삭제했습니다",
        failedDelete: "API 키 삭제에 실패했습니다",
        copied: "API 키를 클립보드에 복사했습니다",
        failedCopy: "클립보드 복사에 실패했습니다",
      },
      actions: {
        create: "API 키 생성",
        creating: "생성 중...",
        done: "완료",
        cancel: "취소",
        delete: "삭제",
      },
      dialogs: {
        createTitle: "API 키 생성",
        createDescription: "용도를 기억하기 쉽도록 API 키에 설명적인 이름을 지정하세요.",
        yourNewApiKey: "새 API 키",
        saveNowWarning: "이 키를 지금 저장하세요. 이후에는 다시 볼 수 없습니다.",
        deleteTitle: "API 키를 삭제할까요?",
        deleteDescription: "이 작업은 되돌릴 수 없습니다. 이 키를 사용하던 애플리케이션은 접근 권한을 잃습니다.",
      },
      form: {
        nameLabel: "이름",
        namePlaceholder: "예: 자동화 스크립트",
      },
      validation: {
        nameRequired: "이름은 필수입니다",
      },
      states: {
        loading: "API 키 불러오는 중...",
        empty: "아직 생성된 API 키가 없습니다",
      },
      labels: {
        id: "ID: {{id}}",
        created: "생성일: {{date}}",
        lastUsed: "마지막 사용: {{date}}",
      },
    },
    instances: {
      toasts: {
        failedReorder: "인스턴스 순서 업데이트에 실패했습니다",
      },
      actions: {
        addInstance: "인스턴스 추가",
        addFirstInstance: "첫 인스턴스 추가",
        adding: "추가 중...",
        cancel: "취소",
      },
      states: {
        loading: "인스턴스 불러오는 중...",
        empty: "구성된 인스턴스가 없습니다",
      },
      titleBarSpeeds: {
        label: "제목 표시줄 속도",
        description: "브라우저 제목 표시줄에 다운로드/업로드 속도를 표시합니다.",
      },
      dialogs: {
        addTitle: "인스턴스 추가",
        addDescription: "관리할 qBittorrent 인스턴스를 새로 추가하세요",
      },
    },
    torznabCache: {
      title: "Torznab 검색 캐시",
      description: "최근 Torznab 응답을 재사용해 반복 검색을 줄입니다.",
      status: {
        enabled: "활성화",
        disabled: "비활성화",
      },
      actions: {
        refreshStats: "통계 새로고침",
        saveTtl: "TTL 저장",
        saving: "저장 중...",
      },
      rows: {
        entries: "항목",
        hitCount: "적중 횟수",
        approxSize: "대략적 크기",
        ttl: "TTL",
        newestEntry: "최신 항목",
        lastUsed: "마지막 사용",
      },
      values: {
        notAvailable: "—",
        minutes: "{{count}}분",
      },
      configuration: {
        title: "구성",
        description: "캐시된 검색 결과를 얼마나 오래 유효하게 유지할지 제어합니다.",
        ttlLabel: "캐시 TTL(분)",
        minimumHelp: "최소 {{min}}분(24시간)입니다. 값을 크게 하면 인덱서 부하는 줄지만 결과 최신성은 낮아집니다.",
      },
      toasts: {
        updated: "캐시 TTL을 {{ttl}}분으로 업데이트했습니다",
        failedUpdate: "캐시 TTL 업데이트에 실패했습니다",
        enterValidNumber: "유효한 분 단위 숫자를 입력하세요",
        minimumTtl: "캐시 TTL은 최소 {{min}}분이어야 합니다",
      },
    },
    instancesCard: {
      title: "인스턴스",
      description: "qBittorrent 연결 설정을 관리합니다",
    },
    integrationsCard: {
      title: "ARR 통합",
      description: "외부 ID를 활용한 cross-seed 검색 강화를 위해 Sonarr/Radarr 인스턴스를 구성합니다",
    },
    clientApiCard: {
      title: "클라이언트 프록시 API 키",
      description: "외부 앱이 qui를 통해 qBittorrent 인스턴스에 연결하도록 API 키를 관리합니다",
    },
    apiCard: {
      title: "API 키",
      description: "외부 접근용 API 키를 관리합니다",
      docsTitle: "API 문서 보기",
      docsText: "API 문서",
    },
    externalProgramsCard: {
      title: "외부 프로그램",
      description: "토렌트 컨텍스트 메뉴에서 실행할 외부 프로그램 또는 스크립트를 설정합니다",
    },
    notificationsCard: {
      title: "알림",
      description: "Shoutrrr 지원 서비스를 통해 알림과 상태 업데이트를 전송합니다",
    },
    dateTimeCard: {
      title: "날짜 및 시간 기본 설정",
      description: "시간대, 날짜 형식, 시간 표시 설정을 구성합니다",
    },
    securityCard: {
      changePasswordTitle: "비밀번호 변경",
      changePasswordDescription: "계정 비밀번호를 업데이트합니다",
      browserIntegrationTitle: "브라우저 통합",
      browserIntegrationDescription: "브라우저의 마그넷 링크 처리 방식을 설정합니다",
      browserIntegrationHelp: "브라우저에서 qui를 마그넷 링크 처리기로 등록하세요. 마그넷 링크를 qui에서 바로 열 수 있습니다.",
      registerAsHandler: "처리기로 등록",
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
