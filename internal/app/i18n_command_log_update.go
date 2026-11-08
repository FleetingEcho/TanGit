package app

var commandLogUpdateTranslations = map[string]map[string]string{
	"zh-Hans": {
		"Git, hosting and custom commands run in this workspace, with what they printed.": "此工作区中的 Git、托管和自定义命令及其输出。",
		"Stage, commit, fetch, push, or run a custom command to put it here.":             "暂存、提交、获取、推送或运行自定义命令，相关命令会显示在这里。",
	},
	"zh-Hant": {
		"Git, hosting and custom commands run in this workspace, with what they printed.": "此工作區中的 Git、託管和自訂命令及其輸出。",
		"Stage, commit, fetch, push, or run a custom command to put it here.":             "暫存、提交、取得、推送或執行自訂命令，相關命令會顯示在這裡。",
	},
	"ja": {
		"Git, hosting and custom commands run in this workspace, with what they printed.": "このワークスペースで実行した Git、ホスティング、カスタムコマンドとその出力を一覧表示します。",
		"Stage, commit, fetch, push, or run a custom command to put it here.":             "ステージ、コミット、フェッチ、プッシュ、カスタムコマンドの実行履歴がここに表示されます。",
	},
	"es": {
		"Git, hosting and custom commands run in this workspace, with what they printed.": "Comandos de Git, alojamiento y personalizados ejecutados en este espacio de trabajo, con sus salidas.",
		"Stage, commit, fetch, push, or run a custom command to put it here.":             "Prepara, confirma, obtén, envía o ejecuta un comando personalizado para que aparezca aquí.",
	},
	"fr": {
		"Git, hosting and custom commands run in this workspace, with what they printed.": "Commandes Git, d’hébergement et personnalisées exécutées dans cet espace de travail, avec leurs sorties.",
		"Stage, commit, fetch, push, or run a custom command to put it here.":             "Indexez, validez, récupérez, poussez ou exécutez une commande personnalisée pour l’afficher ici.",
	},
}

func init() {
	for language, entries := range commandLogUpdateTranslations {
		for english, translated := range entries {
			translations[language][english] = translated
		}
	}
}
