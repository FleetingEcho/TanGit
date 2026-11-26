package i18n

var settingsResetTranslations = map[string]map[string]string{
	"zh-Hans": {
		"Reset settings to defaults":          "重置设置为默认值",
		"Reset every setting to its default?": "将所有设置恢复为默认值？",
		"Custom themes, custom commands and SSH profiles are removed. Repository files and Git history are not changed.": "自定义主题、自定义命令和 SSH 配置文件将被移除。仓库文件和 Git 历史不会改变。",
	},
	"zh-Hant": {
		"Reset settings to defaults":          "將設定還原為預設值",
		"Reset every setting to its default?": "將所有設定還原為預設值？",
		"Custom themes, custom commands and SSH profiles are removed. Repository files and Git history are not changed.": "自訂主題、自訂命令和 SSH 設定檔將被移除。儲存庫檔案和 Git 歷史不會改變。",
	},
	"ja": {
		"Reset settings to defaults":          "設定をデフォルトに戻す",
		"Reset every setting to its default?": "すべての設定をデフォルトに戻しますか？",
		"Custom themes, custom commands and SSH profiles are removed. Repository files and Git history are not changed.": "カスタムテーマ、カスタムコマンド、SSH プロファイルは削除されます。リポジトリのファイルと Git 履歴は変更されません。",
	},
	"es": {
		"Reset settings to defaults":          "Restablecer ajustes predeterminados",
		"Reset every setting to its default?": "¿Restablecer todos los ajustes a sus valores predeterminados?",
		"Custom themes, custom commands and SSH profiles are removed. Repository files and Git history are not changed.": "Se eliminarán los temas personalizados, los comandos personalizados y los perfiles SSH. Los archivos del repositorio y el historial de Git no se modificarán.",
	},
	"fr": {
		"Reset settings to defaults":          "Rétablir les paramètres par défaut",
		"Reset every setting to its default?": "Rétablir tous les paramètres par défaut ?",
		"Custom themes, custom commands and SSH profiles are removed. Repository files and Git history are not changed.": "Les thèmes personnalisés, commandes personnalisées et profils SSH seront supprimés. Les fichiers du dépôt et l’historique Git ne seront pas modifiés.",
	},
}

func init() {
	for language, additions := range settingsResetTranslations {
		catalog := Translations[language]
		for english, translated := range additions {
			if _, exists := catalog[english]; !exists {
				catalog[english] = translated
			}
		}
	}
}
