package i18n

// The Rebase TODO dialog: one window for the rebase overview and its editor.
var rebaseModalTranslations = map[string]map[string]string{
	"zh-Hans": {
		"Open Rebase TODO":                         "打开变基待办",
		"Start or inspect a real Git rebase plan.": "开始或查看真实的 Git 变基计划。",
		"Discard this draft and generate another":  "放弃此草稿并重新生成",
		"Back":                        "返回",
		"Back to the rebase overview": "返回变基概览",
	},
	"zh-Hant": {
		"Open Rebase TODO":                         "開啟重定基底待辦",
		"Start or inspect a real Git rebase plan.": "開始或檢視真實的 Git 重定基底計畫。",
		"Discard this draft and generate another":  "捨棄此草稿並重新產生",
		"Back":                        "返回",
		"Back to the rebase overview": "返回重定基底概覽",
	},
	"ja": {
		"Open Rebase TODO":                         "リベース TODO を開く",
		"Start or inspect a real Git rebase plan.": "実際の Git リベース計画を開始または確認します。",
		"Discard this draft and generate another":  "この下書きを破棄して作り直す",
		"Back":                        "戻る",
		"Back to the rebase overview": "リベースの概要に戻る",
	},
	"es": {
		"Open Rebase TODO":                         "Abrir TODO del rebase",
		"Start or inspect a real Git rebase plan.": "Inicia o revisa un plan de rebase real de Git.",
		"Discard this draft and generate another":  "Descartar este borrador y generar otro",
		"Back":                        "Atrás",
		"Back to the rebase overview": "Volver al resumen del rebase",
	},
	"fr": {
		"Open Rebase TODO":                         "Ouvrir le TODO du rebase",
		"Start or inspect a real Git rebase plan.": "Démarrer ou examiner un vrai plan de rebase Git.",
		"Discard this draft and generate another":  "Abandonner ce brouillon et en générer un autre",
		"Back":                        "Retour",
		"Back to the rebase overview": "Retour à l’aperçu du rebase",
	},
}

func init() {
	for language, additions := range rebaseModalTranslations {
		catalog := Translations[language]
		for english, translated := range additions {
			if _, exists := catalog[english]; !exists {
				catalog[english] = translated
			}
		}
	}
}
