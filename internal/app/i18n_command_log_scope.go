package app

// Labels and empty states for the separated, searchable command log.
var commandLogScopeTranslations = map[string]map[string]string{
	"zh-Hans": {
		"Clear the command log search":             "清除命令日志搜索条件",
		"Your commands":                            "您的命令",
		"Background Git commands":                  "后台 Git 命令",
		"Show commands you chose to run":           "显示您选择运行的命令",
		"Show Git and hosting actions from TanGit": "显示 TanGit 运行的 Git 和托管操作",
		"Search commands":                          "搜索命令",
		"Command or output":                        "命令或输出",
		"Clear search":                             "清除搜索",
		"No commands in this section yet.":         "此分区还没有命令。",
		"Your configured and one-off commands appear in Your commands.": "您配置的命令和一次性命令会显示在“您的命令”中。",
		"Git actions from TanGit appear in Background Git commands.":    "TanGit 的 Git 操作会显示在“后台 Git 命令”中。",
		"No commands match your search.":                                "没有命令符合搜索条件。",
		"Try a different search or switch sections.":                    "请尝试其他搜索条件或切换分区。",
	},
	"zh-Hant": {
		"Clear the command log search":             "清除命令記錄搜尋條件",
		"Your commands":                            "您的命令",
		"Background Git commands":                  "背景 Git 命令",
		"Show commands you chose to run":           "顯示您選擇執行的命令",
		"Show Git and hosting actions from TanGit": "顯示 TanGit 執行的 Git 和託管操作",
		"Search commands":                          "搜尋命令",
		"Command or output":                        "命令或輸出",
		"Clear search":                             "清除搜尋",
		"No commands in this section yet.":         "此分區還沒有命令。",
		"Your configured and one-off commands appear in Your commands.": "您設定的命令和單次命令會顯示在「您的命令」中。",
		"Git actions from TanGit appear in Background Git commands.":    "TanGit 的 Git 操作會顯示在「背景 Git 命令」中。",
		"No commands match your search.":                                "沒有命令符合搜尋條件。",
		"Try a different search or switch sections.":                    "請嘗試其他搜尋條件或切換分區。",
	},
	"ja": {
		"Clear the command log search":             "コマンドログの検索をクリア",
		"Your commands":                            "自分のコマンド",
		"Background Git commands":                  "バックグラウンド Git コマンド",
		"Show commands you chose to run":           "選択して実行したコマンドを表示",
		"Show Git and hosting actions from TanGit": "TanGit による Git とホスティング操作を表示",
		"Search commands":                          "コマンドを検索",
		"Command or output":                        "コマンドまたは出力",
		"Clear search":                             "検索をクリア",
		"No commands in this section yet.":         "このセクションにはまだコマンドがありません。",
		"Your configured and one-off commands appear in Your commands.": "設定済みコマンドと単発コマンドは「自分のコマンド」に表示されます。",
		"Git actions from TanGit appear in Background Git commands.":    "TanGit の Git 操作は「バックグラウンド Git コマンド」に表示されます。",
		"No commands match your search.":                                "検索条件に一致するコマンドがありません。",
		"Try a different search or switch sections.":                    "別の検索条件を試すか、セクションを切り替えてください。",
	},
	"es": {
		"Clear the command log search":             "Borrar la búsqueda del registro de comandos",
		"Your commands":                            "Tus comandos",
		"Background Git commands":                  "Comandos Git en segundo plano",
		"Show commands you chose to run":           "Mostrar los comandos que elegiste ejecutar",
		"Show Git and hosting actions from TanGit": "Mostrar acciones de Git y alojamiento desde TanGit",
		"Search commands":                          "Buscar comandos",
		"Command or output":                        "Comando o salida",
		"Clear search":                             "Borrar búsqueda",
		"No commands in this section yet.":         "Todavía no hay comandos en esta sección.",
		"Your configured and one-off commands appear in Your commands.": "Tus comandos configurados y puntuales aparecen en «Tus comandos».",
		"Git actions from TanGit appear in Background Git commands.":    "Las acciones de Git de TanGit aparecen en «Comandos Git en segundo plano».",
		"No commands match your search.":                                "Ningún comando coincide con la búsqueda.",
		"Try a different search or switch sections.":                    "Prueba otra búsqueda o cambia de sección.",
	},
	"fr": {
		"Clear the command log search":             "Effacer la recherche du journal des commandes",
		"Your commands":                            "Vos commandes",
		"Background Git commands":                  "Commandes Git en arrière-plan",
		"Show commands you chose to run":           "Afficher les commandes que vous avez choisi d’exécuter",
		"Show Git and hosting actions from TanGit": "Afficher les actions Git et d’hébergement de TanGit",
		"Search commands":                          "Rechercher des commandes",
		"Command or output":                        "Commande ou sortie",
		"Clear search":                             "Effacer la recherche",
		"No commands in this section yet.":         "Aucune commande dans cette section pour le moment.",
		"Your configured and one-off commands appear in Your commands.": "Vos commandes configurées et ponctuelles apparaissent dans « Vos commandes ».",
		"Git actions from TanGit appear in Background Git commands.":    "Les actions Git de TanGit apparaissent dans « Commandes Git en arrière-plan ».",
		"No commands match your search.":                                "Aucune commande ne correspond à votre recherche.",
		"Try a different search or switch sections.":                    "Essayez une autre recherche ou changez de section.",
	},
}

func init() {
	for language, entries := range commandLogScopeTranslations {
		for english, translated := range entries {
			translations[language][english] = translated
		}
	}
}
