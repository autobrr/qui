export const crossSeedTableResources = {
  en: {
    columns: {
      name: "Name",
      instance: "Instance",
      match: "Match",
      tracker: "Tracker",
      status: "Status",
      progress: "Progress",
      size: "Size",
      savePath: "Save Path",
    },
    badges: {
      hardlink: "Hardlink",
    },
    tooltips: {
      hardlinkDirectory: "Files stored in hardlink directory (separate from source)",
    },
    status: {
      unregistered: "Unregistered",
      trackerDown: "Tracker Down",
    },
    matchType: {
      content: {
        label: "Content",
        description: "Same content location on disk",
      },
      name: {
        label: "Name",
        description: "Same torrent name",
      },
      release: {
        label: "Release",
        description: "Same release (matched by metadata)",
      },
    },
    values: {
      notAvailable: "-",
    },
    toasts: {
      savePathCopied: "Save path copied",
      failedCopy: "Failed to copy",
    },
    empty: {
      noMatches: "No matching torrents found on other instances",
    },
    toolbar: {
      selectedCount: "{{selected}} of {{total}} selected",
      matchCount: "{{count}} match{{plural}}",
    },
    actions: {
      deselect: "Deselect",
      deleteSelected: "Delete ({{count}})",
      selectAll: "Select All",
      deleteThis: "Delete This",
    },
  },
  "zh-CN": {
    columns: {
      name: "名称",
      instance: "实例",
      match: "匹配",
      tracker: "Tracker",
      status: "状态",
      progress: "进度",
      size: "大小",
      savePath: "保存路径",
    },
    badges: {
      hardlink: "硬链接",
    },
    tooltips: {
      hardlinkDirectory: "文件存储在硬链接目录中（与源目录分离）",
    },
    status: {
      unregistered: "未注册",
      trackerDown: "Tracker 不可用",
    },
    matchType: {
      content: {
        label: "内容",
        description: "磁盘上的内容位置相同",
      },
      name: {
        label: "名称",
        description: "种子名称相同",
      },
      release: {
        label: "版本",
        description: "同一发行版本（基于元数据匹配）",
      },
    },
    values: {
      notAvailable: "-",
    },
    toasts: {
      savePathCopied: "保存路径已复制",
      failedCopy: "复制失败",
    },
    empty: {
      noMatches: "未在其他实例中找到匹配的种子",
    },
    toolbar: {
      selectedCount: "已选择 {{selected}} / {{total}}",
      matchCount: "{{count}} 个匹配",
    },
    actions: {
      deselect: "取消选择",
      deleteSelected: "删除（{{count}}）",
      selectAll: "全选",
      deleteThis: "删除当前",
    },
  },
  ja: {
    columns: {
      name: "名前",
      instance: "インスタンス",
      match: "一致",
      tracker: "トラッカー",
      status: "状態",
      progress: "進捗",
      size: "サイズ",
      savePath: "保存先パス",
    },
    badges: {
      hardlink: "ハードリンク",
    },
    tooltips: {
      hardlinkDirectory: "ファイルはハードリンク用ディレクトリに保存されています（元ディレクトリとは別）",
    },
    status: {
      unregistered: "未登録",
      trackerDown: "トラッカー停止",
    },
    matchType: {
      content: {
        label: "内容",
        description: "ディスク上の内容保存場所が同じ",
      },
      name: {
        label: "名前",
        description: "トレント名が同じ",
      },
      release: {
        label: "リリース",
        description: "同一リリース（メタデータで一致）",
      },
    },
    values: {
      notAvailable: "-",
    },
    toasts: {
      savePathCopied: "保存先パスをコピーしました",
      failedCopy: "コピーに失敗しました",
    },
    empty: {
      noMatches: "他のインスタンスに一致するトレントが見つかりません",
    },
    toolbar: {
      selectedCount: "{{total}} 件中 {{selected}} 件を選択",
      matchCount: "一致 {{count}} 件",
    },
    actions: {
      deselect: "選択解除",
      deleteSelected: "削除（{{count}}）",
      selectAll: "すべて選択",
      deleteThis: "この項目を削除",
    },
  },
  "pt-BR": {
    columns: {
      name: "Nome",
      instance: "Instância",
      match: "Correspondência",
      tracker: "Tracker",
      status: "Status",
      progress: "Progresso",
      size: "Tamanho",
      savePath: "Caminho de salvamento",
    },
    badges: {
      hardlink: "Hardlink",
    },
    tooltips: {
      hardlinkDirectory: "Arquivos armazenados no diretório de hardlink (separado da origem)",
    },
    status: {
      unregistered: "Não registrado",
      trackerDown: "Tracker indisponível",
    },
    matchType: {
      content: {
        label: "Conteúdo",
        description: "Mesma localização de conteúdo no disco",
      },
      name: {
        label: "Nome",
        description: "Mesmo nome de torrent",
      },
      release: {
        label: "Lançamento",
        description: "Mesmo lançamento (correspondência por metadados)",
      },
    },
    values: {
      notAvailable: "-",
    },
    toasts: {
      savePathCopied: "Caminho de salvamento copiado",
      failedCopy: "Falha ao copiar",
    },
    empty: {
      noMatches: "Nenhum torrent correspondente encontrado em outras instâncias",
    },
    toolbar: {
      selectedCount: "{{selected}} de {{total}} selecionados",
      matchCount: "{{count}} correspondência{{plural}}",
    },
    actions: {
      deselect: "Desmarcar",
      deleteSelected: "Excluir ({{count}})",
      selectAll: "Selecionar tudo",
      deleteThis: "Excluir este",
    },
  },
  de: {
    columns: {
      name: "Name",
      instance: "Instanz",
      match: "Treffer",
      tracker: "Tracker",
      status: "Status",
      progress: "Fortschritt",
      size: "Größe",
      savePath: "Speicherpfad",
    },
    badges: {
      hardlink: "Hardlink",
    },
    tooltips: {
      hardlinkDirectory: "Dateien werden im Hardlink-Verzeichnis gespeichert (getrennt vom Quellpfad)",
    },
    status: {
      unregistered: "Nicht registriert",
      trackerDown: "Tracker nicht erreichbar",
    },
    matchType: {
      content: {
        label: "Inhalt",
        description: "Gleicher Speicherort der Inhalte auf der Festplatte",
      },
      name: {
        label: "Name",
        description: "Gleicher Torrent-Name",
      },
      release: {
        label: "Release",
        description: "Gleiches Release (über Metadaten abgeglichen)",
      },
    },
    values: {
      notAvailable: "-",
    },
    toasts: {
      savePathCopied: "Speicherpfad kopiert",
      failedCopy: "Kopieren fehlgeschlagen",
    },
    empty: {
      noMatches: "Keine passenden Torrents auf anderen Instanzen gefunden",
    },
    toolbar: {
      selectedCount: "{{selected}} von {{total}} ausgewählt",
      matchCount: "{{count}} Treffer",
    },
    actions: {
      deselect: "Auswahl aufheben",
      deleteSelected: "Löschen ({{count}})",
      selectAll: "Alle auswählen",
      deleteThis: "Diesen löschen",
    },
  },
} as const
