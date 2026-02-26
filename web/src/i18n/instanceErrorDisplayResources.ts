export const instanceErrorDisplayResources = {
  en: {
    passwordRequired: "Password Required",
    decryptDescription: "Unable to decrypt saved password. This usually happens when the session secret has changed.",
    reenterPassword: "Re-enter Password",
    recentErrorsTitle: "Recent Errors ({{count}})",
  },
  "zh-CN": {
    passwordRequired: "需要密码",
    decryptDescription: "无法解密已保存的密码。这通常是因为会话密钥已更改。",
    reenterPassword: "重新输入密码",
    recentErrorsTitle: "最近错误（{{count}}）",
  },
  ja: {
    passwordRequired: "パスワードが必要",
    decryptDescription: "保存済みパスワードを復号できません。通常はセッションシークレットが変更された場合に発生します。",
    reenterPassword: "パスワードを再入力",
    recentErrorsTitle: "最近のエラー（{{count}}）",
  },
  "pt-BR": {
    passwordRequired: "Senha necessária",
    decryptDescription: "Não foi possível descriptografar a senha salva. Isso geralmente acontece quando o segredo da sessão mudou.",
    reenterPassword: "Inserir senha novamente",
    recentErrorsTitle: "Erros recentes ({{count}})",
  },
  de: {
    passwordRequired: "Passwort erforderlich",
    decryptDescription: "Gespeichertes Passwort konnte nicht entschlüsselt werden. Das passiert meist, wenn sich das Session-Secret geändert hat.",
    reenterPassword: "Passwort erneut eingeben",
    recentErrorsTitle: "Letzte Fehler ({{count}})",
  },
} as const
