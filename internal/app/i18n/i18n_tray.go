package i18n

// Tray menu: status line, recent repositories, Settings and updates.
var trayTranslations = map[string]map[string]string{
	"zh-Hans": {
		"How long the command ran": "命令运行了多久",
		"%d changed":               "%d 个更改", "%d agent tasks need attention": "%d 个 agent 任务需要处理",
		"Recent repositories": "最近的仓库", "Settings…": "设置…", "Check for updates…": "检查更新…",
	},
	"zh-Hant": {
		"How long the command ran": "命令執行了多久",
		"%d changed":               "%d 個變更", "%d agent tasks need attention": "%d 個 agent 任務需要處理",
		"Recent repositories": "最近的儲存庫", "Settings…": "設定…", "Check for updates…": "檢查更新…",
	},
	"ja": {
		"How long the command ran": "コマンドの実行時間",
		"%d changed":               "%d 件の変更", "%d agent tasks need attention": "%d 件のエージェントタスクが対応待ちです",
		"Recent repositories": "最近のリポジトリ", "Settings…": "設定…", "Check for updates…": "アップデートを確認…",
	},
	"es": {
		"How long the command ran": "Cuánto tardó el comando",
		"%d changed":               "%d cambiados", "%d agent tasks need attention": "%d tareas de agente requieren atención",
		"Recent repositories": "Repositorios recientes", "Settings…": "Ajustes…", "Check for updates…": "Buscar actualizaciones…",
	},
	"fr": {
		"How long the command ran": "Durée d’exécution de la commande",
		"%d changed":               "%d modifiés", "%d agent tasks need attention": "%d tâches d’agent demandent votre attention",
		"Recent repositories": "Dépôts récents", "Settings…": "Réglages…", "Check for updates…": "Rechercher des mises à jour…",
	},
}

func init() {
	for language, additions := range trayTranslations {
		catalog := Translations[language]
		for english, translated := range additions {
			// Words other screens already translate keep their existing text.
			if _, exists := catalog[english]; !exists {
				catalog[english] = translated
			}
		}
	}
}
