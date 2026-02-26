export const peersTableResources = {
  en: {
    columns: {
      address: "IP:Port",
      client: "Client",
      progress: "Progress",
      downloadSpeed: "DL Speed",
      uploadSpeed: "UL Speed",
      downloaded: "Downloaded",
      uploaded: "Uploaded",
      flags: "Flags",
    },
    values: {
      notAvailable: "-",
    },
    toasts: {
      ipCopied: "IP address copied to clipboard",
    },
    empty: {
      noPeersConnected: "No peers connected",
    },
    actions: {
      copyIpAddress: "Copy IP Address",
      banPeer: "Ban Peer",
    },
  },
  "zh-CN": {
    columns: {
      address: "IP:端口",
      client: "客户端",
      progress: "进度",
      downloadSpeed: "下载速度",
      uploadSpeed: "上传速度",
      downloaded: "已下载",
      uploaded: "已上传",
      flags: "标记",
    },
    values: {
      notAvailable: "-",
    },
    toasts: {
      ipCopied: "IP 地址已复制到剪贴板",
    },
    empty: {
      noPeersConnected: "当前无已连接用户",
    },
    actions: {
      copyIpAddress: "复制 IP 地址",
      banPeer: "封禁用户",
    },
  },
  ja: {
    columns: {
      address: "IP:ポート",
      client: "クライアント",
      progress: "進捗",
      downloadSpeed: "DL 速度",
      uploadSpeed: "UL 速度",
      downloaded: "ダウンロード済み",
      uploaded: "アップロード済み",
      flags: "フラグ",
    },
    values: {
      notAvailable: "-",
    },
    toasts: {
      ipCopied: "IP アドレスをクリップボードにコピーしました",
    },
    empty: {
      noPeersConnected: "接続中のピアはいません",
    },
    actions: {
      copyIpAddress: "IP アドレスをコピー",
      banPeer: "ピアを禁止",
    },
  },
  "pt-BR": {
    columns: {
      address: "IP:Porta",
      client: "Cliente",
      progress: "Progresso",
      downloadSpeed: "Velocidade DL",
      uploadSpeed: "Velocidade UL",
      downloaded: "Baixado",
      uploaded: "Enviado",
      flags: "Sinalizadores",
    },
    values: {
      notAvailable: "-",
    },
    toasts: {
      ipCopied: "Endereço IP copiado para a área de transferência",
    },
    empty: {
      noPeersConnected: "Nenhum peer conectado",
    },
    actions: {
      copyIpAddress: "Copiar endereço IP",
      banPeer: "Banir peer",
    },
  },
  de: {
    columns: {
      address: "IP:Port",
      client: "Client",
      progress: "Fortschritt",
      downloadSpeed: "DL-Geschwindigkeit",
      uploadSpeed: "UL-Geschwindigkeit",
      downloaded: "Heruntergeladen",
      uploaded: "Hochgeladen",
      flags: "Flags",
    },
    values: {
      notAvailable: "-",
    },
    toasts: {
      ipCopied: "IP-Adresse in die Zwischenablage kopiert",
    },
    empty: {
      noPeersConnected: "Keine Peers verbunden",
    },
    actions: {
      copyIpAddress: "IP-Adresse kopieren",
      banPeer: "Peer sperren",
    },
  },
} as const
