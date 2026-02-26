export interface LocaleNamespaceOverrides {
  common?: Record<string, unknown>
  auth?: Record<string, unknown>
  footer?: Record<string, unknown>
}

export const es419Overrides: LocaleNamespaceOverrides = {
  common: {
    loading: "Cargando...",
    nav: {
      dashboard: "Panel",
      search: "Buscar",
      crossSeed: "Cross-Seed",
      automations: "Automatizaciones",
      backups: "Respaldos",
      rss: "RSS",
      settings: "Configuración",
      logs: "Registros",
      torrents: "Torrents",
    },
    sidebar: {
      show: "Mostrar barra lateral",
      hide: "Ocultar barra lateral",
    },
    header: {
      unified: "Unificado",
      unifiedScope: "Alcance unificado",
      allActive: "Todos activos ({{count}})",
      instances: "Instancias",
      scope: "Alcance",
      switchScope: "Cambiar alcance",
      activeCount: "{{count}} activas",
      activeInstancesSummary: "{{count}} instancia(s) activa(s)",
      noActiveInstances: "No hay instancias activas",
      allInstancesDisabled: "Todas las instancias están deshabilitadas",
      noInstancesConfigured: "No hay instancias configuradas",
      connected: "Conectado",
      disconnected: "Desconectado",
      rssRunning: "RSS en ejecución",
      rssEnabled: "RSS habilitado",
      scanRunning: "Escaneo en ejecución",
      showFilters: "Mostrar filtros",
      hideFilters: "Ocultar filtros",
      addTorrent: "Agregar torrent",
      createTorrent: "Crear torrent",
      torrentCreationTasks: "Tareas de creación de torrents",
      instanceSettings: "Configuración de instancia",
      clearSearch: "Limpiar búsqueda",
      globPlaceholder: "Patrón glob...",
      searchPlaceholder: "Buscar torrents... ({{shortcut}})",
      smartSearchTitle: "Funciones de búsqueda inteligente:",
      smartSearchGlob: "Patrones glob: *.mkv, *1080p*, *S??E??*",
      smartSearchFuzzy: "Búsqueda difusa: \"breaking bad\" encuentra \"Breaking.Bad\"",
      smartSearchNormalize: "Reconoce puntos, guiones bajos y corchetes",
      smartSearchFields: "Busca en nombre, categoría y etiquetas",
      smartSearchEnter: "Presiona Enter para buscar al instante",
      smartSearchAuto: "Busca automáticamente tras 500 ms de pausa",
      currentInstanceAria: "Alcance actual: {{instanceName}}. Haz clic para cambiar.",
    },
    mobile: {
      clients: "Clientes",
      qbittorrentClients: "Clientes de qBittorrent",
      generalSettings: "Configuración general",
    },
    update: {
      available: "Actualización disponible",
      version: "Versión {{version}}",
      viewRelease: "Ver lanzamiento",
      dismiss: "Descartar",
    },
    actions: {
      logout: "Cerrar sesión",
    },
    theme: {
      changeTheme: "Cambiar tema",
      appearance: "Apariencia",
      mode: "Modo",
      light: "Claro",
      dark: "Oscuro",
      system: "Sistema",
      theme: "Tema",
      premium: "Premium",
      switchedMode: "Modo cambiado a {{mode}}",
      switchedTheme: "Tema cambiado a {{theme}}",
      switchedThemeVariation: "Tema cambiado a {{theme}} ({{variation}})",
      unableVerifyLicense: "No se pudo verificar la licencia",
      verifyLicenseDescription: "Falló la comprobación de licencia. El cambio de temas premium no está disponible temporalmente.",
      premiumThemeLocked: "Este es un tema premium. Abre Configuración -> Temas para activar una licencia.",
    },
    languageSwitcher: {
      triggerLabel: "Cambiar idioma",
      menuLabel: "Idioma",
      option: {
        en: "English",
        zhCN: "Chino simplificado",
        ja: "Japonés",
        ptBR: "Portugués (Brasil)",
        de: "Alemán",
        es419: "Español (Latinoamérica)",
        fr: "Francés",
        ko: "Coreano",
      },
    },
    torrentsPage: {
      filtersTitle: "Filtros",
      detailsTitle: "Detalles del torrent",
      detailsTitleWithName: "Detalles del torrent: {{name}}",
    },
    automationsPage: {
      title: "Automatizaciones",
      description: "Servicios de automatización por instancia administrados por qui.",
      noInstances: "Todavía no hay instancias configuradas. Agrega una en Configuración para usar servicios.",
    },
    torrentGeneral: {
      toasts: {
        copied: "{{label}} copiado al portapapeles",
        copyFailed: "No se pudo copiar al portapapeles",
      },
    },
    scrollToTopButton: {
      aria: {
        scrollToTop: "Ir arriba",
      },
    },
    pathCell: {
      aria: {
        copyPath: "Copiar ruta",
      },
      tooltips: {
        copyPath: "Copiar ruta",
      },
      toasts: {
        pathCopied: "Ruta copiada al portapapeles",
        failedCopy: "No se pudo copiar al portapapeles",
      },
    },
    multiSelect: {
      trigger: {
        selectItems: "Seleccionar elementos...",
        title: "Seleccionar",
      },
      searchPlaceholder: "Buscar...",
      empty: {
        create: "Crear \"{{value}}\"",
        noResults: "No se encontraron resultados.",
      },
    },
    dialog: {
      closeSrLabel: "Cerrar",
    },
    sheet: {
      closeSrLabel: "Cerrar",
    },
    fileTree: {
      collapseButton: {
        toggleSrLabel: "Alternar",
      },
    },
    conditionGroup: {
      aria: {
        dragGroup: "Arrastrar grupo",
      },
      operatorHelp: {
        and: "Todas las condiciones deben cumplirse",
        or: "Cualquiera de las condiciones puede cumplirse",
      },
      actions: {
        addCondition: "Condición",
        addGroup: "Grupo",
      },
    },
    categorySubmenu: {
      actions: {
        setCategory: "Definir categoría",
      },
      values: {
        noCategory: "(Sin categoría)",
      },
      search: {
        placeholder: "Buscar categorías...",
        noResults: "No se encontraron categorías",
      },
    },
    webSeedsTable: {
      columns: {
        url: "URL",
      },
      toasts: {
        urlCopied: "URL copiada al portapapeles",
      },
      empty: {
        noHttpSources: "No hay fuentes HTTP",
      },
      search: {
        placeholder: "Buscar URLs...",
      },
      toolbar: {
        filteredCount: "{{filtered}} de {{total}}",
        totalCount: "{{count}} fuente{{plural}} HTTP",
      },
      actions: {
        copyUrl: "Copiar URL",
      },
    },
    crossSeedTable: {
      columns: {
        name: "Nombre",
        instance: "Instancia",
        match: "Coincidencia",
        tracker: "Tracker",
        status: "Estado",
        progress: "Progreso",
        size: "Tamaño",
        savePath: "Ruta de guardado",
      },
      badges: {
        hardlink: "Enlace duro",
      },
      tooltips: {
        hardlinkDirectory: "Archivos guardados en el directorio de hardlinks (separado del origen)",
      },
      status: {
        unregistered: "No registrado",
        trackerDown: "Tracker caído",
      },
      matchType: {
        content: {
          label: "Contenido",
          description: "Misma ubicación de contenido en disco",
        },
        name: {
          label: "Nombre",
          description: "Mismo nombre de torrent",
        },
        release: {
          label: "Release",
          description: "Mismo release (coincidencia por metadatos)",
        },
      },
      values: {
        notAvailable: "-",
      },
      toasts: {
        savePathCopied: "Ruta de guardado copiada",
        failedCopy: "No se pudo copiar",
      },
      empty: {
        noMatches: "No se encontraron torrents coincidentes en otras instancias",
      },
      toolbar: {
        selectedCount: "{{selected}} de {{total}} seleccionados",
        matchCount: "{{count}} coincidencia{{plural}}",
      },
      actions: {
        deselect: "Deseleccionar",
        deleteSelected: "Eliminar ({{count}})",
        selectAll: "Seleccionar todo",
        deleteThis: "Eliminar este",
      },
    },
    torrentContextMenu: {
      values: {
        mixed: "Mixto",
        ellipsis: "...",
      },
      labels: {
        withCount: "{{label}} ({{count}})",
        withMixedCount: "{{label}} ({{count}} {{mixedLabel}})",
        mixedOnly: "({{mixedLabel}})",
      },
      actions: {
        viewDetails: "Ver detalles",
        resume: "Reanudar",
        pause: "Pausar",
        forceRecheck: "Forzar verificación",
        reannounce: "Reanunciar",
        forceStart: "Forzar inicio",
        disableForceStart: "Desactivar inicio forzado",
        enableSequentialDownload: "Activar descarga secuencial",
        disableSequentialDownload: "Desactivar descarga secuencial",
        searchCrossSeeds: "Buscar cross-seeds",
        addTags: "Agregar etiquetas",
        replaceTags: "Reemplazar etiquetas",
        setLocation: "Definir ubicación",
        setShareLimits: "Definir límites de compartición",
        setSpeedLimits: "Definir límites de velocidad",
        enableTmm: "Activar TMM",
        disableTmm: "Desactivar TMM",
        exportTorrent: "Exportar torrent",
        exportTorrents: "Exportar torrents ({{count}})",
        delete: "Eliminar",
      },
      copy: {
        menu: "Copiar...",
        actions: {
          copyName: "Copiar nombre",
          copyHash: "Copiar hash",
          copyFullPath: "Copiar ruta completa",
        },
        types: {
          name: "nombre",
          hash: "hash",
          fullPath: "ruta completa",
        },
        typesPlural: {
          name: "nombres",
          hash: "hashes",
          fullPath: "rutas completas",
        },
      },
      toasts: {
        copied: "Se copió {{item}} del torrent al portapapeles",
        failedCopy: "No se pudo copiar al portapapeles",
        nameNotAvailable: "Nombre no disponible",
        hashNotAvailable: "Hash no disponible",
        fullPathNotAvailable: "Ruta completa no disponible",
        failedFetchNames: "No se pudieron obtener los nombres de torrents",
        failedFetchHashes: "No se pudieron obtener los hashes de torrents",
        failedFetchPaths: "No se pudieron obtener las rutas de torrents",
      },
      filterCrossSeeds: {
        defaultLabel: "Filtrar cross-seeds",
        singleSelectionLabel: "Filtrar cross-seeds (solo selección única)",
        singleSelectionTitle: "El filtrado de cross-seeds solo funciona con un único torrent seleccionado",
      },
      externalPrograms: {
        title: "Programas externos",
        loading: "Cargando programas...",
        toasts: {
          executedAllSuccess: "Programa externo ejecutado correctamente para {{successCount}} torrent(s)",
          executedAllFailed: "No se pudo ejecutar el programa externo para los {{failureCount}} torrent(s)",
          executedPartial: "Ejecutado para {{successCount}} torrent(s), falló para {{failureCount}}",
          executeFailed: "No se pudo ejecutar el programa externo: {{message}}",
        },
      },
    },
    peersTable: {
      columns: {
        address: "IP:Puerto",
        client: "Cliente",
        progress: "Progreso",
        downloadSpeed: "Velocidad DL",
        uploadSpeed: "Velocidad UL",
        downloaded: "Descargado",
        uploaded: "Subido",
        flags: "Banderas",
      },
      values: {
        notAvailable: "-",
      },
      toasts: {
        ipCopied: "Dirección IP copiada al portapapeles",
      },
      empty: {
        noPeersConnected: "No hay peers conectados",
      },
      actions: {
        copyIpAddress: "Copiar dirección IP",
        banPeer: "Bloquear peer",
      },
    },
    torrentDropZone: {
      overlayMessage: "Suelta archivos .torrent o enlaces magnet para agregar",
      toasts: {
        invalidDrop: "Suelta un archivo .torrent o un enlace magnet para agregar",
      },
    },
    statRow: {
      copyAria: "Copiar {{label}}",
    },
  },
  auth: {
    login: {
      subtitle: "Interfaz de gestión de qBittorrent",
      usernameLabel: "Usuario",
      usernamePlaceholder: "Ingresa tu usuario",
      passwordLabel: "Contraseña",
      passwordPlaceholder: "Ingresa tu contraseña",
      rememberMe: "Recordarme",
      signIn: "Iniciar sesión",
      signingIn: "Iniciando sesión...",
      oidcSeparator: "O continuar con",
      oidcButton: "OpenID Connect",
      recoveredSession: "La sesión SSO se actualizó. Inicia sesión de nuevo.",
      errors: {
        usernameRequired: "El usuario es obligatorio",
        passwordRequired: "La contraseña es obligatoria",
        oidcFailed: "Falló la autenticación OIDC",
        invalidCredentials: "Usuario o contraseña inválidos",
        loginFailed: "Falló el inicio de sesión. Intenta nuevamente.",
      },
    },
    setup: {
      subtitle: "Crea tu cuenta para comenzar",
      usernameLabel: "Usuario",
      usernamePlaceholder: "Elige un usuario",
      passwordLabel: "Contraseña",
      passwordPlaceholder: "Elige una contraseña segura",
      confirmPasswordLabel: "Confirmar contraseña",
      confirmPasswordPlaceholder: "Confirma tu contraseña",
      createAccount: "Crear cuenta",
      creatingAccount: "Creando cuenta...",
      errors: {
        usernameRequired: "El usuario es obligatorio",
        usernameTooShort: "El usuario debe tener al menos 3 caracteres",
        passwordRequired: "La contraseña es obligatoria",
        passwordTooShort: "La contraseña debe tener al menos 8 caracteres",
        confirmPasswordRequired: "Confirma tu contraseña",
        passwordMismatch: "Las contraseñas no coinciden",
        createUserFailed: "No se pudo crear el usuario",
      },
    },
  },
  footer: {
    githubAriaLabel: "Ver en GitHub",
  },
}

export const frOverrides: LocaleNamespaceOverrides = {
  common: {
    loading: "Chargement...",
    nav: {
      dashboard: "Tableau de bord",
      search: "Recherche",
      crossSeed: "Cross-Seed",
      automations: "Automatisations",
      backups: "Sauvegardes",
      rss: "RSS",
      settings: "Paramètres",
      logs: "Journaux",
      torrents: "Torrents",
    },
    sidebar: {
      show: "Afficher la barre latérale",
      hide: "Masquer la barre latérale",
    },
    header: {
      unified: "Unifié",
      unifiedScope: "Portée unifiée",
      allActive: "Toutes actives ({{count}})",
      instances: "Instances",
      scope: "Portée",
      switchScope: "Changer la portée",
      activeCount: "{{count}} actives",
      activeInstancesSummary: "{{count}} instance(s) active(s)",
      noActiveInstances: "Aucune instance active",
      allInstancesDisabled: "Toutes les instances sont désactivées",
      noInstancesConfigured: "Aucune instance configurée",
      connected: "Connecté",
      disconnected: "Déconnecté",
      rssRunning: "RSS en cours",
      rssEnabled: "RSS activé",
      scanRunning: "Analyse en cours",
      showFilters: "Afficher les filtres",
      hideFilters: "Masquer les filtres",
      addTorrent: "Ajouter un torrent",
      createTorrent: "Créer un torrent",
      torrentCreationTasks: "Tâches de création de torrent",
      instanceSettings: "Paramètres de l'instance",
      clearSearch: "Effacer la recherche",
      globPlaceholder: "Motif glob...",
      searchPlaceholder: "Rechercher des torrents... ({{shortcut}})",
      smartSearchTitle: "Fonctions de recherche intelligente :",
      smartSearchGlob: "Motifs glob : *.mkv, *1080p*, *S??E??*",
      smartSearchFuzzy: "Correspondance floue : \"breaking bad\" trouve \"Breaking.Bad\"",
      smartSearchNormalize: "Gère les points, tirets bas et crochets",
      smartSearchFields: "Recherche dans le nom, la catégorie et les tags",
      smartSearchEnter: "Appuyez sur Entrée pour une recherche instantanée",
      smartSearchAuto: "Recherche auto après 500 ms d'inactivité",
      currentInstanceAria: "Portée actuelle : {{instanceName}}. Cliquez pour changer.",
    },
    mobile: {
      clients: "Clients",
      qbittorrentClients: "Clients qBittorrent",
      generalSettings: "Paramètres généraux",
    },
    update: {
      available: "Mise à jour disponible",
      version: "Version {{version}}",
      viewRelease: "Voir la version",
      dismiss: "Ignorer",
    },
    actions: {
      logout: "Se déconnecter",
    },
    theme: {
      changeTheme: "Changer le thème",
      appearance: "Apparence",
      mode: "Mode",
      light: "Clair",
      dark: "Sombre",
      system: "Système",
      theme: "Thème",
      premium: "Premium",
      switchedMode: "Mode changé en {{mode}}",
      switchedTheme: "Thème changé en {{theme}}",
      switchedThemeVariation: "Thème changé en {{theme}} ({{variation}})",
      unableVerifyLicense: "Impossible de vérifier la licence",
      verifyLicenseDescription: "La vérification de licence a échoué. Le changement de thème premium est temporairement indisponible.",
      premiumThemeLocked: "Ceci est un thème premium. Ouvrez Paramètres -> Thèmes pour activer une licence.",
    },
    languageSwitcher: {
      triggerLabel: "Changer de langue",
      menuLabel: "Langue",
      option: {
        en: "English",
        zhCN: "Chinois simplifié",
        ja: "Japonais",
        ptBR: "Portugais (Brésil)",
        de: "Allemand",
        es419: "Espagnol (Amérique latine)",
        fr: "Français",
        ko: "Coréen",
      },
    },
    torrentsPage: {
      filtersTitle: "Filtres",
      detailsTitle: "Détails du torrent",
      detailsTitleWithName: "Détails du torrent : {{name}}",
    },
    automationsPage: {
      title: "Automatisations",
      description: "Services d'automatisation par instance gérés par qui.",
      noInstances: "Aucune instance configurée pour le moment. Ajoutez-en une dans Paramètres pour utiliser les services.",
    },
    torrentGeneral: {
      toasts: {
        copied: "{{label}} copié dans le presse-papiers",
        copyFailed: "Échec de la copie dans le presse-papiers",
      },
    },
    scrollToTopButton: {
      aria: {
        scrollToTop: "Remonter en haut",
      },
    },
    pathCell: {
      aria: {
        copyPath: "Copier le chemin",
      },
      tooltips: {
        copyPath: "Copier le chemin",
      },
      toasts: {
        pathCopied: "Chemin copié dans le presse-papiers",
        failedCopy: "Échec de la copie dans le presse-papiers",
      },
    },
    multiSelect: {
      trigger: {
        selectItems: "Sélectionner des éléments...",
        title: "Sélectionner",
      },
      searchPlaceholder: "Rechercher...",
      empty: {
        create: "Créer \"{{value}}\"",
        noResults: "Aucun résultat trouvé.",
      },
    },
    dialog: {
      closeSrLabel: "Fermer",
    },
    sheet: {
      closeSrLabel: "Fermer",
    },
    fileTree: {
      collapseButton: {
        toggleSrLabel: "Basculer",
      },
    },
    conditionGroup: {
      aria: {
        dragGroup: "Faire glisser le groupe",
      },
      operatorHelp: {
        and: "Toutes les conditions doivent correspondre",
        or: "Au moins une condition doit correspondre",
      },
      actions: {
        addCondition: "Condition",
        addGroup: "Groupe",
      },
    },
    categorySubmenu: {
      actions: {
        setCategory: "Définir la catégorie",
      },
      values: {
        noCategory: "(Aucune catégorie)",
      },
      search: {
        placeholder: "Rechercher des catégories...",
        noResults: "Aucune catégorie trouvée",
      },
    },
    webSeedsTable: {
      columns: {
        url: "URL",
      },
      toasts: {
        urlCopied: "URL copiée dans le presse-papiers",
      },
      empty: {
        noHttpSources: "Aucune source HTTP",
      },
      search: {
        placeholder: "Rechercher des URL...",
      },
      toolbar: {
        filteredCount: "{{filtered}} sur {{total}}",
        totalCount: "{{count}} source{{plural}} HTTP",
      },
      actions: {
        copyUrl: "Copier l'URL",
      },
    },
    crossSeedTable: {
      columns: {
        name: "Nom",
        instance: "Instance",
        match: "Correspondance",
        tracker: "Tracker",
        status: "Statut",
        progress: "Progression",
        size: "Taille",
        savePath: "Chemin d'enregistrement",
      },
      badges: {
        hardlink: "Lien physique",
      },
      tooltips: {
        hardlinkDirectory: "Fichiers stockés dans le dossier de liens physiques (séparé de la source)",
      },
      status: {
        unregistered: "Non enregistré",
        trackerDown: "Tracker indisponible",
      },
      matchType: {
        content: {
          label: "Contenu",
          description: "Même emplacement de contenu sur le disque",
        },
        name: {
          label: "Nom",
          description: "Même nom de torrent",
        },
        release: {
          label: "Release",
          description: "Même release (correspondance via métadonnées)",
        },
      },
      values: {
        notAvailable: "-",
      },
      toasts: {
        savePathCopied: "Chemin d'enregistrement copié",
        failedCopy: "Échec de la copie",
      },
      empty: {
        noMatches: "Aucun torrent correspondant trouvé sur les autres instances",
      },
      toolbar: {
        selectedCount: "{{selected}} sur {{total}} sélectionnés",
        matchCount: "{{count}} correspondance{{plural}}",
      },
      actions: {
        deselect: "Désélectionner",
        deleteSelected: "Supprimer ({{count}})",
        selectAll: "Tout sélectionner",
        deleteThis: "Supprimer celui-ci",
      },
    },
    torrentContextMenu: {
      values: {
        mixed: "Mixte",
        ellipsis: "...",
      },
      labels: {
        withCount: "{{label}} ({{count}})",
        withMixedCount: "{{label}} ({{count}} {{mixedLabel}})",
        mixedOnly: "({{mixedLabel}})",
      },
      actions: {
        viewDetails: "Voir les détails",
        resume: "Reprendre",
        pause: "Mettre en pause",
        forceRecheck: "Forcer la revérification",
        reannounce: "Réannoncer",
        forceStart: "Forcer le démarrage",
        disableForceStart: "Désactiver le démarrage forcé",
        enableSequentialDownload: "Activer le téléchargement séquentiel",
        disableSequentialDownload: "Désactiver le téléchargement séquentiel",
        searchCrossSeeds: "Rechercher des cross-seeds",
        addTags: "Ajouter des tags",
        replaceTags: "Remplacer les tags",
        setLocation: "Définir l'emplacement",
        setShareLimits: "Définir les limites de partage",
        setSpeedLimits: "Définir les limites de vitesse",
        enableTmm: "Activer TMM",
        disableTmm: "Désactiver TMM",
        exportTorrent: "Exporter le torrent",
        exportTorrents: "Exporter les torrents ({{count}})",
        delete: "Supprimer",
      },
      copy: {
        menu: "Copier...",
        actions: {
          copyName: "Copier le nom",
          copyHash: "Copier le hash",
          copyFullPath: "Copier le chemin complet",
        },
        types: {
          name: "nom",
          hash: "hash",
          fullPath: "chemin complet",
        },
        typesPlural: {
          name: "noms",
          hash: "hashes",
          fullPath: "chemins complets",
        },
      },
      toasts: {
        copied: "{{item}} du torrent copié dans le presse-papiers",
        failedCopy: "Échec de la copie dans le presse-papiers",
        nameNotAvailable: "Nom indisponible",
        hashNotAvailable: "Hash indisponible",
        fullPathNotAvailable: "Chemin complet indisponible",
        failedFetchNames: "Échec de récupération des noms de torrents",
        failedFetchHashes: "Échec de récupération des hashes de torrents",
        failedFetchPaths: "Échec de récupération des chemins de torrents",
      },
      filterCrossSeeds: {
        defaultLabel: "Filtrer les cross-seeds",
        singleSelectionLabel: "Filtrer les cross-seeds (sélection unique uniquement)",
        singleSelectionTitle: "Le filtrage cross-seed ne fonctionne qu'avec un seul torrent sélectionné",
      },
      externalPrograms: {
        title: "Programmes externes",
        loading: "Chargement des programmes...",
        toasts: {
          executedAllSuccess: "Programme externe exécuté avec succès pour {{successCount}} torrent(s)",
          executedAllFailed: "Échec d'exécution du programme externe pour les {{failureCount}} torrent(s)",
          executedPartial: "Exécuté pour {{successCount}} torrent(s), échec pour {{failureCount}}",
          executeFailed: "Échec d'exécution du programme externe : {{message}}",
        },
      },
    },
    peersTable: {
      columns: {
        address: "IP:Port",
        client: "Client",
        progress: "Progression",
        downloadSpeed: "Vitesse DL",
        uploadSpeed: "Vitesse UL",
        downloaded: "Téléchargé",
        uploaded: "Envoyé",
        flags: "Drapeaux",
      },
      values: {
        notAvailable: "-",
      },
      toasts: {
        ipCopied: "Adresse IP copiée dans le presse-papiers",
      },
      empty: {
        noPeersConnected: "Aucun pair connecté",
      },
      actions: {
        copyIpAddress: "Copier l'adresse IP",
        banPeer: "Bannir le pair",
      },
    },
    torrentDropZone: {
      overlayMessage: "Déposez des fichiers .torrent ou des liens magnet pour ajouter",
      toasts: {
        invalidDrop: "Déposez un fichier .torrent ou un lien magnet pour l'ajouter",
      },
    },
    statRow: {
      copyAria: "Copier {{label}}",
    },
  },
  auth: {
    login: {
      subtitle: "Interface de gestion qBittorrent",
      usernameLabel: "Nom d'utilisateur",
      usernamePlaceholder: "Saisissez votre nom d'utilisateur",
      passwordLabel: "Mot de passe",
      passwordPlaceholder: "Saisissez votre mot de passe",
      rememberMe: "Se souvenir de moi",
      signIn: "Se connecter",
      signingIn: "Connexion en cours...",
      oidcSeparator: "Ou continuer avec",
      oidcButton: "OpenID Connect",
      recoveredSession: "Session SSO actualisée. Veuillez vous reconnecter.",
      errors: {
        usernameRequired: "Le nom d'utilisateur est requis",
        passwordRequired: "Le mot de passe est requis",
        oidcFailed: "Échec de l'authentification OIDC",
        invalidCredentials: "Nom d'utilisateur ou mot de passe invalide",
        loginFailed: "Échec de la connexion. Veuillez réessayer.",
      },
    },
    setup: {
      subtitle: "Créez votre compte pour commencer",
      usernameLabel: "Nom d'utilisateur",
      usernamePlaceholder: "Choisissez un nom d'utilisateur",
      passwordLabel: "Mot de passe",
      passwordPlaceholder: "Choisissez un mot de passe robuste",
      confirmPasswordLabel: "Confirmer le mot de passe",
      confirmPasswordPlaceholder: "Confirmez votre mot de passe",
      createAccount: "Créer le compte",
      creatingAccount: "Création du compte...",
      errors: {
        usernameRequired: "Le nom d'utilisateur est requis",
        usernameTooShort: "Le nom d'utilisateur doit contenir au moins 3 caractères",
        passwordRequired: "Le mot de passe est requis",
        passwordTooShort: "Le mot de passe doit contenir au moins 8 caractères",
        confirmPasswordRequired: "Veuillez confirmer votre mot de passe",
        passwordMismatch: "Les mots de passe ne correspondent pas",
        createUserFailed: "Impossible de créer l'utilisateur",
      },
    },
  },
  footer: {
    githubAriaLabel: "Voir sur GitHub",
  },
}

export const koOverrides: LocaleNamespaceOverrides = {
  common: {
    loading: "로딩 중...",
    nav: {
      dashboard: "대시보드",
      search: "검색",
      crossSeed: "크로스시드",
      automations: "자동화",
      backups: "백업",
      rss: "RSS",
      settings: "설정",
      logs: "로그",
      torrents: "토렌트",
    },
    sidebar: {
      show: "사이드바 표시",
      hide: "사이드바 숨기기",
    },
    header: {
      unified: "통합",
      unifiedScope: "통합 범위",
      allActive: "전체 활성 ({{count}})",
      instances: "인스턴스",
      scope: "범위",
      switchScope: "범위 전환",
      activeCount: "활성 {{count}}개",
      activeInstancesSummary: "활성 인스턴스 {{count}}개",
      noActiveInstances: "활성 인스턴스가 없습니다",
      allInstancesDisabled: "모든 인스턴스가 비활성화되었습니다",
      noInstancesConfigured: "구성된 인스턴스가 없습니다",
      connected: "연결됨",
      disconnected: "연결 끊김",
      rssRunning: "RSS 실행 중",
      rssEnabled: "RSS 활성화됨",
      scanRunning: "스캔 실행 중",
      showFilters: "필터 표시",
      hideFilters: "필터 숨기기",
      addTorrent: "토렌트 추가",
      createTorrent: "토렌트 생성",
      torrentCreationTasks: "토렌트 생성 작업",
      instanceSettings: "인스턴스 설정",
      clearSearch: "검색 지우기",
      globPlaceholder: "Glob 패턴...",
      searchPlaceholder: "토렌트 검색... ({{shortcut}})",
      smartSearchTitle: "스마트 검색 기능:",
      smartSearchGlob: "Glob 패턴: *.mkv, *1080p*, *S??E??*",
      smartSearchFuzzy: "퍼지 매칭: \"breaking bad\"로 \"Breaking.Bad\" 검색",
      smartSearchNormalize: "점, 밑줄, 대괄호를 자동 처리",
      smartSearchFields: "이름, 카테고리, 태그를 검색",
      smartSearchEnter: "Enter로 즉시 검색",
      smartSearchAuto: "500ms 멈추면 자동 검색",
      currentInstanceAria: "현재 범위: {{instanceName}}. 클릭해 변경하세요.",
    },
    mobile: {
      clients: "클라이언트",
      qbittorrentClients: "qBittorrent 클라이언트",
      generalSettings: "일반 설정",
    },
    update: {
      available: "업데이트 가능",
      version: "버전 {{version}}",
      viewRelease: "릴리스 보기",
      dismiss: "닫기",
    },
    actions: {
      logout: "로그아웃",
    },
    theme: {
      changeTheme: "테마 변경",
      appearance: "화면 모양",
      mode: "모드",
      light: "라이트",
      dark: "다크",
      system: "시스템",
      theme: "테마",
      premium: "프리미엄",
      switchedMode: "{{mode}} 모드로 전환됨",
      switchedTheme: "{{theme}} 테마로 전환됨",
      switchedThemeVariation: "{{theme}} 테마 ({{variation}})로 전환됨",
      unableVerifyLicense: "라이선스를 확인할 수 없습니다",
      verifyLicenseDescription: "라이선스 확인에 실패했습니다. 프리미엄 테마 전환을 일시적으로 사용할 수 없습니다.",
      premiumThemeLocked: "프리미엄 테마입니다. 설정 -> 테마에서 라이선스를 활성화하세요.",
    },
    languageSwitcher: {
      triggerLabel: "언어 변경",
      menuLabel: "언어",
      option: {
        en: "English",
        zhCN: "중국어 (간체)",
        ja: "일본어",
        ptBR: "포르투갈어 (브라질)",
        de: "독일어",
        es419: "스페인어 (중남미)",
        fr: "프랑스어",
        ko: "한국어",
      },
    },
    torrentsPage: {
      filtersTitle: "필터",
      detailsTitle: "토렌트 상세",
      detailsTitleWithName: "토렌트 상세: {{name}}",
    },
    automationsPage: {
      title: "자동화",
      description: "qui가 관리하는 인스턴스 단위 자동화 서비스입니다.",
      noInstances: "아직 구성된 인스턴스가 없습니다. 서비스를 사용하려면 설정에서 인스턴스를 추가하세요.",
    },
    torrentGeneral: {
      toasts: {
        copied: "{{label}}을(를) 클립보드에 복사했습니다",
        copyFailed: "클립보드 복사에 실패했습니다",
      },
    },
    scrollToTopButton: {
      aria: {
        scrollToTop: "맨 위로 스크롤",
      },
    },
    pathCell: {
      aria: {
        copyPath: "경로 복사",
      },
      tooltips: {
        copyPath: "경로 복사",
      },
      toasts: {
        pathCopied: "경로를 클립보드에 복사했습니다",
        failedCopy: "클립보드 복사에 실패했습니다",
      },
    },
    multiSelect: {
      trigger: {
        selectItems: "항목 선택...",
        title: "선택",
      },
      searchPlaceholder: "검색...",
      empty: {
        create: "\"{{value}}\" 생성",
        noResults: "검색 결과가 없습니다.",
      },
    },
    dialog: {
      closeSrLabel: "닫기",
    },
    sheet: {
      closeSrLabel: "닫기",
    },
    fileTree: {
      collapseButton: {
        toggleSrLabel: "전환",
      },
    },
    conditionGroup: {
      aria: {
        dragGroup: "그룹 드래그",
      },
      operatorHelp: {
        and: "모든 조건이 일치해야 합니다",
        or: "조건 중 하나만 일치해도 됩니다",
      },
      actions: {
        addCondition: "조건",
        addGroup: "그룹",
      },
    },
    categorySubmenu: {
      actions: {
        setCategory: "카테고리 설정",
      },
      values: {
        noCategory: "(카테고리 없음)",
      },
      search: {
        placeholder: "카테고리 검색...",
        noResults: "카테고리를 찾을 수 없습니다",
      },
    },
    webSeedsTable: {
      columns: {
        url: "URL",
      },
      toasts: {
        urlCopied: "URL을 클립보드에 복사했습니다",
      },
      empty: {
        noHttpSources: "HTTP 소스가 없습니다",
      },
      search: {
        placeholder: "URL 검색...",
      },
      toolbar: {
        filteredCount: "{{total}}개 중 {{filtered}}개",
        totalCount: "HTTP 소스 {{count}}개",
      },
      actions: {
        copyUrl: "URL 복사",
      },
    },
    crossSeedTable: {
      columns: {
        name: "이름",
        instance: "인스턴스",
        match: "일치",
        tracker: "트래커",
        status: "상태",
        progress: "진행률",
        size: "크기",
        savePath: "저장 경로",
      },
      badges: {
        hardlink: "하드링크",
      },
      tooltips: {
        hardlinkDirectory: "파일이 하드링크 디렉터리에 저장됨 (원본 경로와 분리)",
      },
      status: {
        unregistered: "미등록",
        trackerDown: "트래커 다운",
      },
      matchType: {
        content: {
          label: "콘텐츠",
          description: "디스크 내 콘텐츠 위치가 동일함",
        },
        name: {
          label: "이름",
          description: "토렌트 이름이 동일함",
        },
        release: {
          label: "릴리스",
          description: "동일한 릴리스 (메타데이터 기준 매칭)",
        },
      },
      values: {
        notAvailable: "-",
      },
      toasts: {
        savePathCopied: "저장 경로를 복사했습니다",
        failedCopy: "복사에 실패했습니다",
      },
      empty: {
        noMatches: "다른 인스턴스에서 일치하는 토렌트를 찾지 못했습니다",
      },
      toolbar: {
        selectedCount: "{{total}}개 중 {{selected}}개 선택됨",
        matchCount: "일치 {{count}}개",
      },
      actions: {
        deselect: "선택 해제",
        deleteSelected: "삭제 ({{count}})",
        selectAll: "전체 선택",
        deleteThis: "이 항목 삭제",
      },
    },
    torrentContextMenu: {
      values: {
        mixed: "혼합",
        ellipsis: "...",
      },
      labels: {
        withCount: "{{label}} ({{count}})",
        withMixedCount: "{{label}} ({{count}} {{mixedLabel}})",
        mixedOnly: "({{mixedLabel}})",
      },
      actions: {
        viewDetails: "상세 보기",
        resume: "재개",
        pause: "일시 정지",
        forceRecheck: "강제 재검사",
        reannounce: "재공지",
        forceStart: "강제 시작",
        disableForceStart: "강제 시작 비활성화",
        enableSequentialDownload: "순차 다운로드 활성화",
        disableSequentialDownload: "순차 다운로드 비활성화",
        searchCrossSeeds: "크로스시드 검색",
        addTags: "태그 추가",
        replaceTags: "태그 교체",
        setLocation: "위치 설정",
        setShareLimits: "공유 제한 설정",
        setSpeedLimits: "속도 제한 설정",
        enableTmm: "TMM 활성화",
        disableTmm: "TMM 비활성화",
        exportTorrent: "토렌트 내보내기",
        exportTorrents: "토렌트 내보내기 ({{count}})",
        delete: "삭제",
      },
      copy: {
        menu: "복사...",
        actions: {
          copyName: "이름 복사",
          copyHash: "해시 복사",
          copyFullPath: "전체 경로 복사",
        },
        types: {
          name: "이름",
          hash: "해시",
          fullPath: "전체 경로",
        },
        typesPlural: {
          name: "이름",
          hash: "해시",
          fullPath: "전체 경로",
        },
      },
      toasts: {
        copied: "토렌트 {{item}}을(를) 클립보드에 복사했습니다",
        failedCopy: "클립보드 복사에 실패했습니다",
        nameNotAvailable: "이름을 사용할 수 없습니다",
        hashNotAvailable: "해시를 사용할 수 없습니다",
        fullPathNotAvailable: "전체 경로를 사용할 수 없습니다",
        failedFetchNames: "토렌트 이름을 불러오지 못했습니다",
        failedFetchHashes: "토렌트 해시를 불러오지 못했습니다",
        failedFetchPaths: "토렌트 경로를 불러오지 못했습니다",
      },
      filterCrossSeeds: {
        defaultLabel: "크로스시드 필터",
        singleSelectionLabel: "크로스시드 필터 (단일 선택만)",
        singleSelectionTitle: "크로스시드 필터는 단일 토렌트 선택에서만 동작합니다",
      },
      externalPrograms: {
        title: "외부 프로그램",
        loading: "프로그램 로딩 중...",
        toasts: {
          executedAllSuccess: "{{successCount}}개 토렌트에 외부 프로그램 실행 완료",
          executedAllFailed: "{{failureCount}}개 토렌트 모두 실행 실패",
          executedPartial: "{{successCount}}개 실행, {{failureCount}}개 실패",
          executeFailed: "외부 프로그램 실행 실패: {{message}}",
        },
      },
    },
    peersTable: {
      columns: {
        address: "IP:포트",
        client: "클라이언트",
        progress: "진행률",
        downloadSpeed: "DL 속도",
        uploadSpeed: "UL 속도",
        downloaded: "다운로드됨",
        uploaded: "업로드됨",
        flags: "플래그",
      },
      values: {
        notAvailable: "-",
      },
      toasts: {
        ipCopied: "IP 주소를 클립보드에 복사했습니다",
      },
      empty: {
        noPeersConnected: "연결된 피어가 없습니다",
      },
      actions: {
        copyIpAddress: "IP 주소 복사",
        banPeer: "피어 차단",
      },
    },
    torrentDropZone: {
      overlayMessage: ".torrent 파일 또는 magnet 링크를 드롭해 추가",
      toasts: {
        invalidDrop: "추가하려면 .torrent 파일 또는 magnet 링크를 드롭하세요",
      },
    },
    statRow: {
      copyAria: "{{label}} 복사",
    },
  },
  auth: {
    login: {
      subtitle: "qBittorrent 관리 인터페이스",
      usernameLabel: "사용자 이름",
      usernamePlaceholder: "사용자 이름을 입력하세요",
      passwordLabel: "비밀번호",
      passwordPlaceholder: "비밀번호를 입력하세요",
      rememberMe: "로그인 상태 유지",
      signIn: "로그인",
      signingIn: "로그인 중...",
      oidcSeparator: "또는 다음으로 계속",
      oidcButton: "OpenID Connect",
      recoveredSession: "SSO 세션이 갱신되었습니다. 다시 로그인하세요.",
      errors: {
        usernameRequired: "사용자 이름은 필수입니다",
        passwordRequired: "비밀번호는 필수입니다",
        oidcFailed: "OIDC 인증에 실패했습니다",
        invalidCredentials: "사용자 이름 또는 비밀번호가 올바르지 않습니다",
        loginFailed: "로그인에 실패했습니다. 다시 시도하세요.",
      },
    },
    setup: {
      subtitle: "시작하려면 계정을 생성하세요",
      usernameLabel: "사용자 이름",
      usernamePlaceholder: "사용자 이름을 선택하세요",
      passwordLabel: "비밀번호",
      passwordPlaceholder: "안전한 비밀번호를 선택하세요",
      confirmPasswordLabel: "비밀번호 확인",
      confirmPasswordPlaceholder: "비밀번호를 다시 입력하세요",
      createAccount: "계정 생성",
      creatingAccount: "계정 생성 중...",
      errors: {
        usernameRequired: "사용자 이름은 필수입니다",
        usernameTooShort: "사용자 이름은 최소 3자여야 합니다",
        passwordRequired: "비밀번호는 필수입니다",
        passwordTooShort: "비밀번호는 최소 8자여야 합니다",
        confirmPasswordRequired: "비밀번호 확인이 필요합니다",
        passwordMismatch: "비밀번호가 일치하지 않습니다",
        createUserFailed: "사용자 생성에 실패했습니다",
      },
    },
  },
  footer: {
    githubAriaLabel: "GitHub에서 보기",
  },
}
