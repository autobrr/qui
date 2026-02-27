export const deleteTorrentDialogResources = {
  en: {
    title: "Delete {{count}} torrent(s)?",
    description: "This action cannot be undone. The torrents will be removed from qBittorrent.",
    totalSize: "Total size: {{size}}",
    blockCrossSeeds: "Block cross-seed infohashes (prevent re-add)",
    actions: {
      cancel: "Cancel",
      delete: "Delete",
    },
  },
  "zh-CN": {
    title: "删除 {{count}} 个种子？",
    description: "此操作无法撤销。种子将从 qBittorrent 中移除。",
    totalSize: "总大小：{{size}}",
    blockCrossSeeds: "阻止 cross-seed 信息哈希（防止再次添加）",
    actions: {
      cancel: "取消",
      delete: "删除",
    },
  },
  ja: {
    title: "{{count}} 件のトレントを削除しますか？",
    description: "この操作は元に戻せません。トレントは qBittorrent から削除されます。",
    totalSize: "合計サイズ: {{size}}",
    blockCrossSeeds: "クロスシードの infohash をブロック（再追加を防止）",
    actions: {
      cancel: "キャンセル",
      delete: "削除",
    },
  },
  "pt-BR": {
    title: "Excluir {{count}} torrent(s)?",
    description: "Esta ação não pode ser desfeita. Os torrents serão removidos do qBittorrent.",
    totalSize: "Tamanho total: {{size}}",
    blockCrossSeeds: "Bloquear infohashes de cross-seed (impedir readição)",
    actions: {
      cancel: "Cancelar",
      delete: "Excluir",
    },
  },
  de: {
    title: "{{count}} Torrent(s) löschen?",
    description: "Diese Aktion kann nicht rückgängig gemacht werden. Die Torrents werden aus qBittorrent entfernt.",
    totalSize: "Gesamtgröße: {{size}}",
    blockCrossSeeds: "Cross-Seed-Infohashes blockieren (erneutes Hinzufügen verhindern)",
    actions: {
      cancel: "Abbrechen",
      delete: "Löschen",
    },
  },
  "es-419": {
    title: "¿Eliminar {{count}} torrent(s)?",
    description: "Esta acción no se puede deshacer. Los torrents se eliminarán de qBittorrent.",
    totalSize: "Tamaño total: {{size}}",
    blockCrossSeeds: "Bloquear infohashes de cross-seed (evitar re-agregado)",
    actions: {
      cancel: "Cancelar",
      delete: "Eliminar",
    },
  },
  fr: {
    title: "Supprimer {{count}} torrent(s) ?",
    description: "Cette action est irréversible. Les torrents seront retirés de qBittorrent.",
    totalSize: "Taille totale : {{size}}",
    blockCrossSeeds: "Bloquer les infohashs cross-seed (éviter la réimportation)",
    actions: {
      cancel: "Annuler",
      delete: "Supprimer",
    },
  },
  ko: {
    title: "{{count}}개 토렌트를 삭제할까요?",
    description: "이 작업은 되돌릴 수 없습니다. 토렌트가 qBittorrent에서 제거됩니다.",
    totalSize: "총 크기: {{size}}",
    blockCrossSeeds: "크로스시드 infohash 차단 (재추가 방지)",
    actions: {
      cancel: "취소",
      delete: "삭제",
    },
  },
} as const
