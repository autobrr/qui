export const preferencesDialogsResources = {
  en: {
    seedingLimitsForm: {
      loading: "Loading seeding limits...",
      loadFailed: "Failed to load preferences",
      toasts: {
        updated: "Seeding limits updated",
        failedUpdate: "Failed to update seeding limits",
      },
      fields: {
        enableShareRatioLimitLabel: "Enable share ratio limit",
        enableShareRatioLimitDescription: "Stop seeding when this ratio is reached.",
        maximumShareRatioLabel: "Maximum share ratio",
        maximumShareRatioDescription: "Stop seeding at this upload/download ratio.",
        enableSeedingTimeLimitLabel: "Enable seeding time limit",
        enableSeedingTimeLimitDescription: "Stop seeding after a time limit.",
        maximumSeedingTimeLabel: "Maximum seeding time (minutes)",
        maximumSeedingTimeDescription: "Stop seeding after this many minutes.",
      },
      actions: {
        saving: "Saving...",
        saveChanges: "Save Changes",
      },
    },
    queueManagementForm: {
      loading: "Loading queue settings...",
      loadFailed: "Failed to load preferences",
      toasts: {
        updated: "Queue settings updated",
        failedUpdate: "Failed to update queue settings",
      },
      fields: {
        enableQueueingLabel: "Enable queueing",
        enableQueueingDescription: "Limit how many torrents can be active at once.",
        maxActiveDownloadsLabel: "Max active downloads",
        maxActiveDownloadsDescription: "Maximum number of downloading torrents.",
        maxActiveUploadsLabel: "Max active uploads",
        maxActiveUploadsDescription: "Maximum number of uploading torrents.",
        maxActiveTorrentsLabel: "Max active torrents",
        maxActiveTorrentsDescription: "Total maximum active torrents.",
        maxCheckingTorrentsLabel: "Max checking torrents",
        maxCheckingTorrentsDescription: "Maximum torrents checking simultaneously.",
      },
      errors: {
        maxActiveDownloadsMin: "Maximum active downloads must be greater than -1",
        maxActiveUploadsMin: "Maximum active uploads must be greater than -1",
        maxActiveTorrentsMin: "Maximum active torrents must be greater than -1",
      },
      actions: {
        saving: "Saving...",
        saveChanges: "Save Changes",
      },
    },
    orphanScanSettingsDialog: {
      title: "Configure Orphan Scan",
      instanceFallback: "Instance",
      actions: {
        saveChanges: "Save Changes",
      },
    },
    orphanScanPreviewDialog: {
      title: "Orphan File Preview",
      description: "Review files that are not associated with any torrent before deletion.",
      summary: {
        filesAndSize: "{{count}} files · {{size}}",
        truncated: "(truncated)",
      },
      table: {
        path: "Path",
        size: "Size",
        modified: "Modified",
        status: "Status",
      },
      states: {
        loading: "Loading...",
        noFiles: "No files to display.",
      },
      pagination: {
        showing: "Showing {{shown}} of {{total}}",
      },
      actions: {
        loadMore: "Load more",
        exportCsv: "Export CSV",
        close: "Close",
        deleteFiles: "Delete Files",
      },
      csv: {
        path: "Path",
        size: "Size",
        sizeBytes: "Size (bytes)",
        modified: "Modified",
      },
      toasts: {
        deletionStarted: {
          title: "Deletion started",
          description: "Orphan files are being removed.",
        },
        failedStartDeletion: {
          title: "Failed to start deletion",
        },
        unknownError: "Unknown error",
        exportedCsv: "Exported {{count}} files to CSV",
        failedExport: "Failed to export files",
      },
    },
    reannounceSettingsDialog: {
      title: "Configure Reannounce",
      instanceFallback: "Instance",
      actions: {
        saving: "Saving...",
        saveChanges: "Save Changes",
      },
    },
  },
  "zh-CN": {
    seedingLimitsForm: {
      loading: "正在加载做种限制...",
      loadFailed: "加载偏好设置失败",
      toasts: {
        updated: "做种限制已更新",
        failedUpdate: "更新做种限制失败",
      },
      fields: {
        enableShareRatioLimitLabel: "启用分享率限制",
        enableShareRatioLimitDescription: "达到该分享率后停止做种。",
        maximumShareRatioLabel: "最大分享率",
        maximumShareRatioDescription: "达到该上传/下载比后停止做种。",
        enableSeedingTimeLimitLabel: "启用做种时长限制",
        enableSeedingTimeLimitDescription: "达到时长上限后停止做种。",
        maximumSeedingTimeLabel: "最大做种时长（分钟）",
        maximumSeedingTimeDescription: "做种达到该分钟数后停止。",
      },
      actions: {
        saving: "正在保存...",
        saveChanges: "保存更改",
      },
    },
    queueManagementForm: {
      loading: "正在加载队列设置...",
      loadFailed: "加载偏好设置失败",
      toasts: {
        updated: "队列设置已更新",
        failedUpdate: "更新队列设置失败",
      },
      fields: {
        enableQueueingLabel: "启用队列",
        enableQueueingDescription: "限制同时处于活动状态的种子数量。",
        maxActiveDownloadsLabel: "最大活动下载数",
        maxActiveDownloadsDescription: "允许同时下载的最大种子数。",
        maxActiveUploadsLabel: "最大活动上传数",
        maxActiveUploadsDescription: "允许同时上传的最大种子数。",
        maxActiveTorrentsLabel: "最大活动种子数",
        maxActiveTorrentsDescription: "活动种子的总数量上限。",
        maxCheckingTorrentsLabel: "最大校验种子数",
        maxCheckingTorrentsDescription: "允许同时进行校验的最大种子数。",
      },
      errors: {
        maxActiveDownloadsMin: "最大活动下载数必须大于 -1",
        maxActiveUploadsMin: "最大活动上传数必须大于 -1",
        maxActiveTorrentsMin: "最大活动种子数必须大于 -1",
      },
      actions: {
        saving: "正在保存...",
        saveChanges: "保存更改",
      },
    },
    orphanScanSettingsDialog: {
      title: "配置孤儿文件扫描",
      instanceFallback: "实例",
      actions: {
        saveChanges: "保存更改",
      },
    },
    orphanScanPreviewDialog: {
      title: "孤儿文件预览",
      description: "删除前请先检查这些未关联任何种子的文件。",
      summary: {
        filesAndSize: "{{count}} 个文件 · {{size}}",
        truncated: "（已截断）",
      },
      table: {
        path: "路径",
        size: "大小",
        modified: "修改时间",
        status: "状态",
      },
      states: {
        loading: "加载中...",
        noFiles: "没有可显示的文件。",
      },
      pagination: {
        showing: "显示 {{shown}} / {{total}}",
      },
      actions: {
        loadMore: "加载更多",
        exportCsv: "导出 CSV",
        close: "关闭",
        deleteFiles: "删除文件",
      },
      csv: {
        path: "路径",
        size: "大小",
        sizeBytes: "大小（字节）",
        modified: "修改时间",
      },
      toasts: {
        deletionStarted: {
          title: "已开始删除",
          description: "孤儿文件正在删除中。",
        },
        failedStartDeletion: {
          title: "启动删除失败",
        },
        unknownError: "未知错误",
        exportedCsv: "已导出 {{count}} 个文件到 CSV",
        failedExport: "导出文件失败",
      },
    },
    reannounceSettingsDialog: {
      title: "配置重新汇报",
      instanceFallback: "实例",
      actions: {
        saving: "正在保存...",
        saveChanges: "保存更改",
      },
    },
  },
  ja: {
    seedingLimitsForm: {
      loading: "シード制限を読み込み中...",
      loadFailed: "設定の読み込みに失敗しました",
      toasts: {
        updated: "シード制限を更新しました",
        failedUpdate: "シード制限の更新に失敗しました",
      },
      fields: {
        enableShareRatioLimitLabel: "共有比率の上限を有効化",
        enableShareRatioLimitDescription: "この比率に達したらシードを停止します。",
        maximumShareRatioLabel: "最大共有比率",
        maximumShareRatioDescription: "このアップロード/ダウンロード比率でシードを停止します。",
        enableSeedingTimeLimitLabel: "シード時間の上限を有効化",
        enableSeedingTimeLimitDescription: "時間上限に達したらシードを停止します。",
        maximumSeedingTimeLabel: "最大シード時間（分）",
        maximumSeedingTimeDescription: "この分数に達したらシードを停止します。",
      },
      actions: {
        saving: "保存中...",
        saveChanges: "変更を保存",
      },
    },
    queueManagementForm: {
      loading: "キュー設定を読み込み中...",
      loadFailed: "設定の読み込みに失敗しました",
      toasts: {
        updated: "キュー設定を更新しました",
        failedUpdate: "キュー設定の更新に失敗しました",
      },
      fields: {
        enableQueueingLabel: "キュー管理を有効化",
        enableQueueingDescription: "同時にアクティブにできるトレント数を制限します。",
        maxActiveDownloadsLabel: "最大アクティブダウンロード数",
        maxActiveDownloadsDescription: "同時ダウンロードの上限数。",
        maxActiveUploadsLabel: "最大アクティブアップロード数",
        maxActiveUploadsDescription: "同時アップロードの上限数。",
        maxActiveTorrentsLabel: "最大アクティブトレント数",
        maxActiveTorrentsDescription: "アクティブトレント全体の上限数。",
        maxCheckingTorrentsLabel: "同時チェック上限トレント数",
        maxCheckingTorrentsDescription: "同時に整合性チェックできるトレントの上限数。",
      },
      errors: {
        maxActiveDownloadsMin: "最大アクティブダウンロード数は -1 より大きくしてください",
        maxActiveUploadsMin: "最大アクティブアップロード数は -1 より大きくしてください",
        maxActiveTorrentsMin: "最大アクティブトレント数は -1 より大きくしてください",
      },
      actions: {
        saving: "保存中...",
        saveChanges: "変更を保存",
      },
    },
    orphanScanSettingsDialog: {
      title: "孤立ファイルスキャンを設定",
      instanceFallback: "インスタンス",
      actions: {
        saveChanges: "変更を保存",
      },
    },
    orphanScanPreviewDialog: {
      title: "孤立ファイルのプレビュー",
      description: "削除前に、どのトレントにも紐付いていないファイルを確認します。",
      summary: {
        filesAndSize: "{{count}} 件のファイル · {{size}}",
        truncated: "（省略表示）",
      },
      table: {
        path: "パス",
        size: "サイズ",
        modified: "更新日時",
        status: "状態",
      },
      states: {
        loading: "読み込み中...",
        noFiles: "表示できるファイルがありません。",
      },
      pagination: {
        showing: "{{total}} 件中 {{shown}} 件を表示",
      },
      actions: {
        loadMore: "さらに読み込む",
        exportCsv: "CSV をエクスポート",
        close: "閉じる",
        deleteFiles: "ファイルを削除",
      },
      csv: {
        path: "パス",
        size: "サイズ",
        sizeBytes: "サイズ（バイト）",
        modified: "更新日時",
      },
      toasts: {
        deletionStarted: {
          title: "削除を開始しました",
          description: "孤立ファイルを削除しています。",
        },
        failedStartDeletion: {
          title: "削除の開始に失敗しました",
        },
        unknownError: "不明なエラー",
        exportedCsv: "{{count}} 件のファイルを CSV にエクスポートしました",
        failedExport: "ファイルのエクスポートに失敗しました",
      },
    },
    reannounceSettingsDialog: {
      title: "再アナウンスを設定",
      instanceFallback: "インスタンス",
      actions: {
        saving: "保存中...",
        saveChanges: "変更を保存",
      },
    },
  },
  "pt-BR": {
    seedingLimitsForm: {
      loading: "Carregando limites de seed...",
      loadFailed: "Falha ao carregar preferências",
      toasts: {
        updated: "Limites de seed atualizados",
        failedUpdate: "Falha ao atualizar limites de seed",
      },
      fields: {
        enableShareRatioLimitLabel: "Ativar limite de ratio",
        enableShareRatioLimitDescription: "Para de semear quando esse ratio for atingido.",
        maximumShareRatioLabel: "Ratio máximo",
        maximumShareRatioDescription: "Para de semear nesta proporção de upload/download.",
        enableSeedingTimeLimitLabel: "Ativar limite de tempo de seed",
        enableSeedingTimeLimitDescription: "Para de semear após atingir o limite de tempo.",
        maximumSeedingTimeLabel: "Tempo máximo de seed (minutos)",
        maximumSeedingTimeDescription: "Para de semear após esta quantidade de minutos.",
      },
      actions: {
        saving: "Salvando...",
        saveChanges: "Salvar alterações",
      },
    },
    queueManagementForm: {
      loading: "Carregando configurações de fila...",
      loadFailed: "Falha ao carregar preferências",
      toasts: {
        updated: "Configurações de fila atualizadas",
        failedUpdate: "Falha ao atualizar configurações de fila",
      },
      fields: {
        enableQueueingLabel: "Ativar fila",
        enableQueueingDescription: "Limita quantos torrents podem ficar ativos ao mesmo tempo.",
        maxActiveDownloadsLabel: "Máx. downloads ativos",
        maxActiveDownloadsDescription: "Número máximo de torrents em download.",
        maxActiveUploadsLabel: "Máx. uploads ativos",
        maxActiveUploadsDescription: "Número máximo de torrents em upload.",
        maxActiveTorrentsLabel: "Máx. torrents ativos",
        maxActiveTorrentsDescription: "Limite total de torrents ativos.",
        maxCheckingTorrentsLabel: "Máx. torrents em verificação",
        maxCheckingTorrentsDescription: "Máximo de torrents verificando simultaneamente.",
      },
      errors: {
        maxActiveDownloadsMin: "O máximo de downloads ativos deve ser maior que -1",
        maxActiveUploadsMin: "O máximo de uploads ativos deve ser maior que -1",
        maxActiveTorrentsMin: "O máximo de torrents ativos deve ser maior que -1",
      },
      actions: {
        saving: "Salvando...",
        saveChanges: "Salvar alterações",
      },
    },
    orphanScanSettingsDialog: {
      title: "Configurar varredura de órfãos",
      instanceFallback: "Instância",
      actions: {
        saveChanges: "Salvar alterações",
      },
    },
    orphanScanPreviewDialog: {
      title: "Prévia de arquivos órfãos",
      description: "Revise arquivos sem associação com torrents antes de excluir.",
      summary: {
        filesAndSize: "{{count}} arquivos · {{size}}",
        truncated: "(truncado)",
      },
      table: {
        path: "Caminho",
        size: "Tamanho",
        modified: "Modificado",
        status: "Status",
      },
      states: {
        loading: "Carregando...",
        noFiles: "Nenhum arquivo para exibir.",
      },
      pagination: {
        showing: "Mostrando {{shown}} de {{total}}",
      },
      actions: {
        loadMore: "Carregar mais",
        exportCsv: "Exportar CSV",
        close: "Fechar",
        deleteFiles: "Excluir arquivos",
      },
      csv: {
        path: "Caminho",
        size: "Tamanho",
        sizeBytes: "Tamanho (bytes)",
        modified: "Modificado",
      },
      toasts: {
        deletionStarted: {
          title: "Exclusão iniciada",
          description: "Os arquivos órfãos estão sendo removidos.",
        },
        failedStartDeletion: {
          title: "Falha ao iniciar exclusão",
        },
        unknownError: "Erro desconhecido",
        exportedCsv: "{{count}} arquivos exportados para CSV",
        failedExport: "Falha ao exportar arquivos",
      },
    },
    reannounceSettingsDialog: {
      title: "Configurar reannounce",
      instanceFallback: "Instância",
      actions: {
        saving: "Salvando...",
        saveChanges: "Salvar alterações",
      },
    },
  },
  "es-419": {
    seedingLimitsForm: {
      loading: "Cargando límites de siembra...",
      loadFailed: "No se pudieron cargar las preferencias",
      toasts: {
        updated: "Límites de siembra actualizados",
        failedUpdate: "No se pudieron actualizar los límites de siembra",
      },
      fields: {
        enableShareRatioLimitLabel: "Habilitar límite de ratio de compartición",
        enableShareRatioLimitDescription: "Detener la siembra al alcanzar este ratio.",
        maximumShareRatioLabel: "Ratio de compartición máximo",
        maximumShareRatioDescription: "Detener la siembra en esta relación subida/descarga.",
        enableSeedingTimeLimitLabel: "Habilitar límite de tiempo de siembra",
        enableSeedingTimeLimitDescription: "Detener la siembra después de un límite de tiempo.",
        maximumSeedingTimeLabel: "Tiempo máximo de siembra (minutos)",
        maximumSeedingTimeDescription: "Detener la siembra después de esta cantidad de minutos.",
      },
      actions: {
        saving: "Guardando...",
        saveChanges: "Guardar cambios",
      },
    },
    queueManagementForm: {
      loading: "Cargando configuración de cola...",
      loadFailed: "No se pudieron cargar las preferencias",
      toasts: {
        updated: "Configuración de cola actualizada",
        failedUpdate: "No se pudo actualizar la configuración de cola",
      },
      fields: {
        enableQueueingLabel: "Habilitar cola",
        enableQueueingDescription: "Limita cuántos torrents pueden estar activos al mismo tiempo.",
        maxActiveDownloadsLabel: "Máx. descargas activas",
        maxActiveDownloadsDescription: "Número máximo de torrents descargando.",
        maxActiveUploadsLabel: "Máx. subidas activas",
        maxActiveUploadsDescription: "Número máximo de torrents subiendo.",
        maxActiveTorrentsLabel: "Máx. torrents activos",
        maxActiveTorrentsDescription: "Máximo total de torrents activos.",
        maxCheckingTorrentsLabel: "Máx. torrents verificando",
        maxCheckingTorrentsDescription: "Máximo de torrents verificando simultáneamente.",
      },
      errors: {
        maxActiveDownloadsMin: "El máximo de descargas activas debe ser mayor que -1",
        maxActiveUploadsMin: "El máximo de subidas activas debe ser mayor que -1",
        maxActiveTorrentsMin: "El máximo de torrents activos debe ser mayor que -1",
      },
      actions: {
        saving: "Guardando...",
        saveChanges: "Guardar cambios",
      },
    },
    orphanScanSettingsDialog: {
      title: "Configurar escaneo de huérfanos",
      instanceFallback: "Instancia",
      actions: {
        saveChanges: "Guardar cambios",
      },
    },
    orphanScanPreviewDialog: {
      title: "Vista previa de archivos huérfanos",
      description: "Revisa archivos no asociados con ningún torrent antes de eliminarlos.",
      summary: {
        filesAndSize: "{{count}} archivos · {{size}}",
        truncated: "(truncado)",
      },
      table: {
        path: "Ruta",
        size: "Tamaño",
        modified: "Modificado",
        status: "Estado",
      },
      states: {
        loading: "Cargando...",
        noFiles: "No hay archivos para mostrar.",
      },
      pagination: {
        showing: "Mostrando {{shown}} de {{total}}",
      },
      actions: {
        loadMore: "Cargar más",
        exportCsv: "Exportar CSV",
        close: "Cerrar",
        deleteFiles: "Eliminar archivos",
      },
      csv: {
        path: "Ruta",
        size: "Tamaño",
        sizeBytes: "Tamaño (bytes)",
        modified: "Modificado",
      },
      toasts: {
        deletionStarted: {
          title: "Eliminación iniciada",
          description: "Los archivos huérfanos se están eliminando.",
        },
        failedStartDeletion: {
          title: "No se pudo iniciar la eliminación",
        },
        unknownError: "Error desconocido",
        exportedCsv: "Se exportaron {{count}} archivos a CSV",
        failedExport: "No se pudieron exportar los archivos",
      },
    },
    reannounceSettingsDialog: {
      title: "Configurar reanuncio",
      instanceFallback: "Instancia",
      actions: {
        saving: "Guardando...",
        saveChanges: "Guardar cambios",
      },
    },
  },
  fr: {
    seedingLimitsForm: {
      loading: "Chargement des limites de seeding...",
      loadFailed: "Échec du chargement des préférences",
      toasts: {
        updated: "Limites de seeding mises à jour",
        failedUpdate: "Échec de la mise à jour des limites de seeding",
      },
      fields: {
        enableShareRatioLimitLabel: "Activer la limite de ratio de partage",
        enableShareRatioLimitDescription: "Arrêter le seeding quand ce ratio est atteint.",
        maximumShareRatioLabel: "Ratio de partage maximal",
        maximumShareRatioDescription: "Arrêter le seeding à ce ratio upload/download.",
        enableSeedingTimeLimitLabel: "Activer la limite de durée de seeding",
        enableSeedingTimeLimitDescription: "Arrêter le seeding après une limite de temps.",
        maximumSeedingTimeLabel: "Durée maximale de seeding (minutes)",
        maximumSeedingTimeDescription: "Arrêter le seeding après ce nombre de minutes.",
      },
      actions: {
        saving: "Enregistrement...",
        saveChanges: "Enregistrer les modifications",
      },
    },
    queueManagementForm: {
      loading: "Chargement des paramètres de file...",
      loadFailed: "Échec du chargement des préférences",
      toasts: {
        updated: "Paramètres de file mis à jour",
        failedUpdate: "Échec de la mise à jour des paramètres de file",
      },
      fields: {
        enableQueueingLabel: "Activer la file",
        enableQueueingDescription: "Limiter le nombre de torrents actifs simultanément.",
        maxActiveDownloadsLabel: "Max téléchargements actifs",
        maxActiveDownloadsDescription: "Nombre maximum de torrents en téléchargement.",
        maxActiveUploadsLabel: "Max envois actifs",
        maxActiveUploadsDescription: "Nombre maximum de torrents en envoi.",
        maxActiveTorrentsLabel: "Max torrents actifs",
        maxActiveTorrentsDescription: "Nombre total maximum de torrents actifs.",
        maxCheckingTorrentsLabel: "Max torrents en vérification",
        maxCheckingTorrentsDescription: "Nombre maximum de torrents vérifiés simultanément.",
      },
      errors: {
        maxActiveDownloadsMin: "Le maximum de téléchargements actifs doit être supérieur à -1",
        maxActiveUploadsMin: "Le maximum d'envois actifs doit être supérieur à -1",
        maxActiveTorrentsMin: "Le maximum de torrents actifs doit être supérieur à -1",
      },
      actions: {
        saving: "Enregistrement...",
        saveChanges: "Enregistrer les modifications",
      },
    },
    orphanScanSettingsDialog: {
      title: "Configurer l'analyse des orphelins",
      instanceFallback: "Instance",
      actions: {
        saveChanges: "Enregistrer les modifications",
      },
    },
    orphanScanPreviewDialog: {
      title: "Aperçu des fichiers orphelins",
      description: "Vérifiez les fichiers non associés à un torrent avant suppression.",
      summary: {
        filesAndSize: "{{count}} fichiers · {{size}}",
        truncated: "(tronqué)",
      },
      table: {
        path: "Chemin",
        size: "Taille",
        modified: "Modifié",
        status: "Statut",
      },
      states: {
        loading: "Chargement...",
        noFiles: "Aucun fichier à afficher.",
      },
      pagination: {
        showing: "{{shown}} sur {{total}} affichés",
      },
      actions: {
        loadMore: "Charger plus",
        exportCsv: "Exporter CSV",
        close: "Fermer",
        deleteFiles: "Supprimer les fichiers",
      },
      csv: {
        path: "Chemin",
        size: "Taille",
        sizeBytes: "Taille (octets)",
        modified: "Modifié",
      },
      toasts: {
        deletionStarted: {
          title: "Suppression lancée",
          description: "Les fichiers orphelins sont en cours de suppression.",
        },
        failedStartDeletion: {
          title: "Échec du démarrage de la suppression",
        },
        unknownError: "Erreur inconnue",
        exportedCsv: "{{count}} fichiers exportés en CSV",
        failedExport: "Échec de l'export des fichiers",
      },
    },
    reannounceSettingsDialog: {
      title: "Configurer le réannonce",
      instanceFallback: "Instance",
      actions: {
        saving: "Enregistrement...",
        saveChanges: "Enregistrer les modifications",
      },
    },
  },
  ko: {
    seedingLimitsForm: {
      loading: "시딩 제한 불러오는 중...",
      loadFailed: "설정을 불러오지 못했습니다",
      toasts: {
        updated: "시딩 제한을 업데이트했습니다",
        failedUpdate: "시딩 제한 업데이트에 실패했습니다",
      },
      fields: {
        enableShareRatioLimitLabel: "공유 비율 제한 활성화",
        enableShareRatioLimitDescription: "이 비율에 도달하면 시딩을 중지합니다.",
        maximumShareRatioLabel: "최대 공유 비율",
        maximumShareRatioDescription: "이 업로드/다운로드 비율에서 시딩을 중지합니다.",
        enableSeedingTimeLimitLabel: "시딩 시간 제한 활성화",
        enableSeedingTimeLimitDescription: "시간 제한에 도달하면 시딩을 중지합니다.",
        maximumSeedingTimeLabel: "최대 시딩 시간(분)",
        maximumSeedingTimeDescription: "이 분 수에 도달하면 시딩을 중지합니다.",
      },
      actions: {
        saving: "저장 중...",
        saveChanges: "변경 사항 저장",
      },
    },
    queueManagementForm: {
      loading: "대기열 설정 불러오는 중...",
      loadFailed: "설정을 불러오지 못했습니다",
      toasts: {
        updated: "대기열 설정을 업데이트했습니다",
        failedUpdate: "대기열 설정 업데이트에 실패했습니다",
      },
      fields: {
        enableQueueingLabel: "대기열 사용",
        enableQueueingDescription: "동시에 활성화할 수 있는 토렌트 수를 제한합니다.",
        maxActiveDownloadsLabel: "최대 활성 다운로드",
        maxActiveDownloadsDescription: "동시에 다운로드 중인 토렌트의 최대 수.",
        maxActiveUploadsLabel: "최대 활성 업로드",
        maxActiveUploadsDescription: "동시에 업로드 중인 토렌트의 최대 수.",
        maxActiveTorrentsLabel: "최대 활성 토렌트",
        maxActiveTorrentsDescription: "전체 활성 토렌트 최대 수.",
        maxCheckingTorrentsLabel: "최대 검사 중 토렌트",
        maxCheckingTorrentsDescription: "동시에 검사할 수 있는 최대 토렌트 수.",
      },
      errors: {
        maxActiveDownloadsMin: "최대 활성 다운로드는 -1보다 커야 합니다",
        maxActiveUploadsMin: "최대 활성 업로드는 -1보다 커야 합니다",
        maxActiveTorrentsMin: "최대 활성 토렌트는 -1보다 커야 합니다",
      },
      actions: {
        saving: "저장 중...",
        saveChanges: "변경 사항 저장",
      },
    },
    orphanScanSettingsDialog: {
      title: "고아 파일 스캔 설정",
      instanceFallback: "인스턴스",
      actions: {
        saveChanges: "변경 사항 저장",
      },
    },
    orphanScanPreviewDialog: {
      title: "고아 파일 미리보기",
      description: "삭제 전에 어떤 토렌트와도 연결되지 않은 파일을 확인하세요.",
      summary: {
        filesAndSize: "{{count}}개 파일 · {{size}}",
        truncated: "(잘림)",
      },
      table: {
        path: "경로",
        size: "크기",
        modified: "수정됨",
        status: "상태",
      },
      states: {
        loading: "불러오는 중...",
        noFiles: "표시할 파일이 없습니다.",
      },
      pagination: {
        showing: "{{total}}개 중 {{shown}}개 표시",
      },
      actions: {
        loadMore: "더 불러오기",
        exportCsv: "CSV 내보내기",
        close: "닫기",
        deleteFiles: "파일 삭제",
      },
      csv: {
        path: "경로",
        size: "크기",
        sizeBytes: "크기 (바이트)",
        modified: "수정됨",
      },
      toasts: {
        deletionStarted: {
          title: "삭제 시작됨",
          description: "고아 파일을 제거하는 중입니다.",
        },
        failedStartDeletion: {
          title: "삭제 시작 실패",
        },
        unknownError: "알 수 없는 오류",
        exportedCsv: "{{count}}개 파일을 CSV로 내보냈습니다",
        failedExport: "파일 내보내기에 실패했습니다",
      },
    },
    reannounceSettingsDialog: {
      title: "재공지 설정",
      instanceFallback: "인스턴스",
      actions: {
        saving: "저장 중...",
        saveChanges: "변경 사항 저장",
      },
    },
  },
  de: {
    seedingLimitsForm: {
      loading: "Seed-Limits werden geladen...",
      loadFailed: "Einstellungen konnten nicht geladen werden",
      toasts: {
        updated: "Seed-Limits aktualisiert",
        failedUpdate: "Seed-Limits konnten nicht aktualisiert werden",
      },
      fields: {
        enableShareRatioLimitLabel: "Share-Ratio-Limit aktivieren",
        enableShareRatioLimitDescription: "Stoppt das Seeding, sobald dieses Verhältnis erreicht ist.",
        maximumShareRatioLabel: "Maximale Share-Ratio",
        maximumShareRatioDescription: "Stoppt das Seeding bei diesem Upload/Download-Verhältnis.",
        enableSeedingTimeLimitLabel: "Seeding-Zeitlimit aktivieren",
        enableSeedingTimeLimitDescription: "Stoppt das Seeding nach Erreichen des Zeitlimits.",
        maximumSeedingTimeLabel: "Maximale Seeding-Zeit (Minuten)",
        maximumSeedingTimeDescription: "Stoppt das Seeding nach dieser Minutenanzahl.",
      },
      actions: {
        saving: "Speichern...",
        saveChanges: "Änderungen speichern",
      },
    },
    queueManagementForm: {
      loading: "Warteschlangen-Einstellungen werden geladen...",
      loadFailed: "Einstellungen konnten nicht geladen werden",
      toasts: {
        updated: "Warteschlangen-Einstellungen aktualisiert",
        failedUpdate: "Warteschlangen-Einstellungen konnten nicht aktualisiert werden",
      },
      fields: {
        enableQueueingLabel: "Warteschlange aktivieren",
        enableQueueingDescription: "Begrenzt, wie viele Torrents gleichzeitig aktiv sein dürfen.",
        maxActiveDownloadsLabel: "Max. aktive Downloads",
        maxActiveDownloadsDescription: "Maximale Anzahl gleichzeitig ladender Torrents.",
        maxActiveUploadsLabel: "Max. aktive Uploads",
        maxActiveUploadsDescription: "Maximale Anzahl gleichzeitig hochladender Torrents.",
        maxActiveTorrentsLabel: "Max. aktive Torrents",
        maxActiveTorrentsDescription: "Gesamtlimit für aktive Torrents.",
        maxCheckingTorrentsLabel: "Max. prüfende Torrents",
        maxCheckingTorrentsDescription: "Maximale Anzahl gleichzeitig prüfender Torrents.",
      },
      errors: {
        maxActiveDownloadsMin: "Maximale aktive Downloads müssen größer als -1 sein",
        maxActiveUploadsMin: "Maximale aktive Uploads müssen größer als -1 sein",
        maxActiveTorrentsMin: "Maximale aktive Torrents müssen größer als -1 sein",
      },
      actions: {
        saving: "Speichern...",
        saveChanges: "Änderungen speichern",
      },
    },
    orphanScanSettingsDialog: {
      title: "Orphan-Scan konfigurieren",
      instanceFallback: "Instanz",
      actions: {
        saveChanges: "Änderungen speichern",
      },
    },
    orphanScanPreviewDialog: {
      title: "Orphan-Dateivorschau",
      description: "Prüfe Dateien ohne Torrent-Zuordnung vor dem Löschen.",
      summary: {
        filesAndSize: "{{count}} Dateien · {{size}}",
        truncated: "(gekürzt)",
      },
      table: {
        path: "Pfad",
        size: "Größe",
        modified: "Geändert",
        status: "Status",
      },
      states: {
        loading: "Lädt...",
        noFiles: "Keine Dateien zum Anzeigen.",
      },
      pagination: {
        showing: "{{shown}} von {{total}} angezeigt",
      },
      actions: {
        loadMore: "Mehr laden",
        exportCsv: "CSV exportieren",
        close: "Schließen",
        deleteFiles: "Dateien löschen",
      },
      csv: {
        path: "Pfad",
        size: "Größe",
        sizeBytes: "Größe (Bytes)",
        modified: "Geändert",
      },
      toasts: {
        deletionStarted: {
          title: "Löschung gestartet",
          description: "Orphan-Dateien werden entfernt.",
        },
        failedStartDeletion: {
          title: "Löschung konnte nicht gestartet werden",
        },
        unknownError: "Unbekannter Fehler",
        exportedCsv: "{{count}} Dateien als CSV exportiert",
        failedExport: "Dateien konnten nicht exportiert werden",
      },
    },
    reannounceSettingsDialog: {
      title: "Reannounce konfigurieren",
      instanceFallback: "Instanz",
      actions: {
        saving: "Speichern...",
        saveChanges: "Änderungen speichern",
      },
    },
  },
} as const
