export const blocklistTabResources = {
  en: {
    titles: {
      main: "Cross-Seed Blocklist",
      blockedHashes: "Blocked Hashes",
    },
    descriptions: {
      noInstances: "Add instances to manage blocked cross-seed infohashes.",
      main: "Prevent specific infohashes from being injected on a per-instance basis.",
      blockedHashes: "Entries are applied only to the selected instance.",
    },
    labels: {
      instance: "Instance",
      infohash: "Infohash",
      noteOptional: "Note (optional)",
    },
    placeholders: {
      selectInstance: "Select instance",
      infohash: "e.g. 63e07ff523710ca268567dad344ce1e0e6b7e8a3",
      note: "Why is this hash blocked?",
    },
    actions: {
      add: "Add",
      removeAria: "Remove {{infoHash}} from blocklist",
    },
    columns: {
      infohash: "Infohash",
      note: "Note",
      added: "Added",
    },
    empty: {
      noBlockedHashes: "No blocked infohashes.",
    },
    values: {
      empty: "—",
    },
    toasts: {
      added: "Added to blocklist",
      removed: "Removed from blocklist",
      selectInstance: "Select an instance",
      invalidInfohash: "Infohash must be 40 or 64 hex characters",
    },
  },
  "zh-CN": {
    titles: {
      main: "Cross-Seed 屏蔽列表",
      blockedHashes: "已屏蔽哈希",
    },
    descriptions: {
      noInstances: "请先添加实例，以管理被屏蔽的 cross-seed infohash。",
      main: "按实例阻止特定 infohash 被注入。",
      blockedHashes: "以下条目仅对当前所选实例生效。",
    },
    labels: {
      instance: "实例",
      infohash: "Infohash",
      noteOptional: "备注（可选）",
    },
    placeholders: {
      selectInstance: "选择实例",
      infohash: "例如 63e07ff523710ca268567dad344ce1e0e6b7e8a3",
      note: "为什么要屏蔽这个哈希？",
    },
    actions: {
      add: "添加",
      removeAria: "将 {{infoHash}} 从屏蔽列表中移除",
    },
    columns: {
      infohash: "Infohash",
      note: "备注",
      added: "添加时间",
    },
    empty: {
      noBlockedHashes: "暂无被屏蔽的 infohash。",
    },
    values: {
      empty: "—",
    },
    toasts: {
      added: "已添加到屏蔽列表",
      removed: "已从屏蔽列表移除",
      selectInstance: "请选择一个实例",
      invalidInfohash: "Infohash 必须为 40 或 64 位十六进制字符",
    },
  },
  ja: {
    titles: {
      main: "Cross-Seed ブロックリスト",
      blockedHashes: "ブロック済みハッシュ",
    },
    descriptions: {
      noInstances: "ブロックする cross-seed の infohash を管理するには、まずインスタンスを追加してください。",
      main: "インスタンス単位で特定の infohash の注入を防止します。",
      blockedHashes: "エントリは選択中のインスタンスにのみ適用されます。",
    },
    labels: {
      instance: "インスタンス",
      infohash: "Infohash",
      noteOptional: "メモ（任意）",
    },
    placeholders: {
      selectInstance: "インスタンスを選択",
      infohash: "例: 63e07ff523710ca268567dad344ce1e0e6b7e8a3",
      note: "このハッシュをブロックする理由",
    },
    actions: {
      add: "追加",
      removeAria: "{{infoHash}} をブロックリストから削除",
    },
    columns: {
      infohash: "Infohash",
      note: "メモ",
      added: "追加日時",
    },
    empty: {
      noBlockedHashes: "ブロックされた infohash はありません。",
    },
    values: {
      empty: "—",
    },
    toasts: {
      added: "ブロックリストに追加しました",
      removed: "ブロックリストから削除しました",
      selectInstance: "インスタンスを選択してください",
      invalidInfohash: "Infohash は 40 文字または 64 文字の16進数である必要があります",
    },
  },
  "pt-BR": {
    titles: {
      main: "Lista de Bloqueio de Cross-Seed",
      blockedHashes: "Hashes Bloqueados",
    },
    descriptions: {
      noInstances: "Adicione instâncias para gerenciar infohashes bloqueados de cross-seed.",
      main: "Impede que infohashes específicos sejam injetados por instância.",
      blockedHashes: "As entradas são aplicadas apenas à instância selecionada.",
    },
    labels: {
      instance: "Instância",
      infohash: "Infohash",
      noteOptional: "Nota (opcional)",
    },
    placeholders: {
      selectInstance: "Selecione a instância",
      infohash: "ex.: 63e07ff523710ca268567dad344ce1e0e6b7e8a3",
      note: "Por que este hash está bloqueado?",
    },
    actions: {
      add: "Adicionar",
      removeAria: "Remover {{infoHash}} da lista de bloqueio",
    },
    columns: {
      infohash: "Infohash",
      note: "Nota",
      added: "Adicionado",
    },
    empty: {
      noBlockedHashes: "Nenhum infohash bloqueado.",
    },
    values: {
      empty: "—",
    },
    toasts: {
      added: "Adicionado à lista de bloqueio",
      removed: "Removido da lista de bloqueio",
      selectInstance: "Selecione uma instância",
      invalidInfohash: "O infohash deve ter 40 ou 64 caracteres hexadecimais",
    },
  },
  de: {
    titles: {
      main: "Cross-Seed-Blocklist",
      blockedHashes: "Blockierte Hashes",
    },
    descriptions: {
      noInstances: "Füge Instanzen hinzu, um blockierte Cross-Seed-Infohashes zu verwalten.",
      main: "Verhindert die Injektion bestimmter Infohashes pro Instanz.",
      blockedHashes: "Einträge gelten nur für die ausgewählte Instanz.",
    },
    labels: {
      instance: "Instanz",
      infohash: "Infohash",
      noteOptional: "Notiz (optional)",
    },
    placeholders: {
      selectInstance: "Instanz auswählen",
      infohash: "z. B. 63e07ff523710ca268567dad344ce1e0e6b7e8a3",
      note: "Warum ist dieser Hash blockiert?",
    },
    actions: {
      add: "Hinzufügen",
      removeAria: "{{infoHash}} aus der Blockliste entfernen",
    },
    columns: {
      infohash: "Infohash",
      note: "Notiz",
      added: "Hinzugefügt",
    },
    empty: {
      noBlockedHashes: "Keine blockierten Infohashes.",
    },
    values: {
      empty: "—",
    },
    toasts: {
      added: "Zur Blockliste hinzugefügt",
      removed: "Aus der Blockliste entfernt",
      selectInstance: "Wähle eine Instanz aus",
      invalidInfohash: "Infohash muss aus 40 oder 64 hexadezimalen Zeichen bestehen",
    },
  },
} as const
