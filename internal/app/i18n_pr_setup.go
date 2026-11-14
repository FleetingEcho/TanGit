package app

// Pull-request setup and no-request feedback added after the feature catalogs.
// Keep these user-facing strings in every language as with the other surfaces.
var pullRequestFeedbackTranslations = map[string]map[string]string{
	"zh-Hans": {
		"Set up %s for pull requests": "设置 %s 以使用拉取请求",
		"Install %s or sign in with '%s auth login', then retry. The CLI output is in the command log.": "请安装 %s 或使用 '%s auth login' 登录，然后重试。CLI 输出见命令日志。",
		"No pull request for this branch":                  "此分支没有拉取请求",
		"Create one to start a review and see its checks.": "创建一个以开始审查并查看检查结果。",
	},
	"zh-Hant": {
		"Set up %s for pull requests": "設定 %s 以使用拉取請求",
		"Install %s or sign in with '%s auth login', then retry. The CLI output is in the command log.": "請安裝 %s 或使用 '%s auth login' 登入，再重試。CLI 輸出可在命令記錄中查看。",
		"No pull request for this branch":                  "此分支沒有拉取請求",
		"Create one to start a review and see its checks.": "建立一個以開始審查並查看檢查結果。",
	},
	"ja": {
		"Set up %s for pull requests": "プルリクエスト用に %s をセットアップ",
		"Install %s or sign in with '%s auth login', then retry. The CLI output is in the command log.": "%s をインストールするか、「%s auth login」でログインしてから、再試行してください。CLI の出力はコマンドログにあります。",
		"No pull request for this branch":                  "このブランチにはプルリクエストがありません",
		"Create one to start a review and see its checks.": "作成すると、レビューとチェック結果を確認できます。",
	},
	"es": {
		"Set up %s for pull requests": "Configura %s para las pull requests",
		"Install %s or sign in with '%s auth login', then retry. The CLI output is in the command log.": "Instala %s o inicia sesión con '%s auth login' y vuelve a intentarlo. La salida de la CLI está en el registro de comandos.",
		"No pull request for this branch":                  "No hay pull request para esta rama",
		"Create one to start a review and see its checks.": "Crea una para iniciar una revisión y ver sus comprobaciones.",
	},
	"fr": {
		"Set up %s for pull requests": "Configurez %s pour les pull requests",
		"Install %s or sign in with '%s auth login', then retry. The CLI output is in the command log.": "Installez %s ou connectez-vous avec '%s auth login', puis réessayez. La sortie de la CLI se trouve dans le journal des commandes.",
		"No pull request for this branch":                  "Aucune pull request pour cette branche",
		"Create one to start a review and see its checks.": "Créez-en une pour lancer une revue et consulter les vérifications.",
	},
}

func init() {
	for language, entries := range pullRequestFeedbackTranslations {
		for english, translated := range entries {
			translations[language][english] = translated
		}
	}
}
