package app

// Toolbar visibility keeps each independently configurable action localized.
var toolbarVisibilityTranslations = map[string]map[string]string{
	"zh-Hans": {
		"Toolbar buttons":                                   "工具栏按钮",
		"Choose which global actions stay visible":          "选择要显示的全局操作",
		"Header toolbar":                                    "顶部工具栏",
		"Repository, editor, and application actions":       "仓库、编辑器和应用操作",
		"Show %s in header":                                 "在顶部工具栏显示%s",
		"Status bar":                                        "状态栏",
		"Command, pull request, terminal, and help actions": "命令、拉取请求、终端和帮助操作",
		"Show %s in status bar":                             "在状态栏显示%s",
	},
	"zh-Hant": {
		"Toolbar buttons":                                   "工具列按鈕",
		"Choose which global actions stay visible":          "選擇要顯示的全域操作",
		"Header toolbar":                                    "頂部工具列",
		"Repository, editor, and application actions":       "儲存庫、編輯器和應用程式操作",
		"Show %s in header":                                 "在頂部工具列顯示%s",
		"Status bar":                                        "狀態列",
		"Command, pull request, terminal, and help actions": "命令、拉取請求、終端和說明操作",
		"Show %s in status bar":                             "在狀態列顯示%s",
	},
	"ja": {
		"Toolbar buttons":                                   "ツールバーボタン",
		"Choose which global actions stay visible":          "表示するグローバル操作を選択",
		"Header toolbar":                                    "ヘッダーツールバー",
		"Repository, editor, and application actions":       "リポジトリ、エディター、アプリケーションの操作",
		"Show %s in header":                                 "ヘッダーに%sを表示",
		"Status bar":                                        "ステータスバー",
		"Command, pull request, terminal, and help actions": "コマンド、プルリクエスト、ターミナル、ヘルプの操作",
		"Show %s in status bar":                             "ステータスバーに%sを表示",
	},
	"es": {
		"Toolbar buttons":                                   "Botones de la barra de herramientas",
		"Choose which global actions stay visible":          "Elige qué acciones globales permanecen visibles",
		"Header toolbar":                                    "Barra de herramientas superior",
		"Repository, editor, and application actions":       "Acciones del repositorio, el editor y la aplicación",
		"Show %s in header":                                 "Mostrar %s en la barra superior",
		"Status bar":                                        "Barra de estado",
		"Command, pull request, terminal, and help actions": "Acciones de comandos, pull requests, terminal y ayuda",
		"Show %s in status bar":                             "Mostrar %s en la barra de estado",
	},
	"fr": {
		"Toolbar buttons":                                   "Boutons de la barre d’outils",
		"Choose which global actions stay visible":          "Choisissez les actions globales à afficher",
		"Header toolbar":                                    "Barre d’outils supérieure",
		"Repository, editor, and application actions":       "Actions du dépôt, de l’éditeur et de l’application",
		"Show %s in header":                                 "Afficher %s dans la barre supérieure",
		"Status bar":                                        "Barre d’état",
		"Command, pull request, terminal, and help actions": "Actions de commandes, pull requests, terminal et aide",
		"Show %s in status bar":                             "Afficher %s dans la barre d’état",
	},
}

func init() {
	for language, additions := range toolbarVisibilityTranslations {
		catalog := translations[language]
		for english, translated := range additions {
			catalog[english] = translated
		}
	}
}
