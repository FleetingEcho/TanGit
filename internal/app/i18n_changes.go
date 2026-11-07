package app

// Changed-file navigation strings live beside the review catalogs so every
// entry point shares one translation key.
var changeWorkflowTranslations = map[string]map[string]string{
	"zh-Hans": {
		"Previous changed file": "上一个更改文件", "Next changed file": "下一个更改文件",
		"Go to the previous changed file (Alt+K)": "转到上一个更改文件 (Alt+K)",
		"Go to the next changed file (Alt+J)":     "转到下一个更改文件 (Alt+J)",
		"Go to the previous changed file":         "转到上一个更改文件", "Go to the next changed file": "转到下一个更改文件",
	},
	"zh-Hant": {
		"Previous changed file": "上一個變更檔案", "Next changed file": "下一個變更檔案",
		"Go to the previous changed file (Alt+K)": "前往上一個變更檔案 (Alt+K)",
		"Go to the next changed file (Alt+J)":     "前往下一個變更檔案 (Alt+J)",
		"Go to the previous changed file":         "前往上一個變更檔案", "Go to the next changed file": "前往下一個變更檔案",
	},
	"ja": {
		"Previous changed file": "前の変更ファイル", "Next changed file": "次の変更ファイル",
		"Go to the previous changed file (Alt+K)": "前の変更ファイルへ移動 (Alt+K)",
		"Go to the next changed file (Alt+J)":     "次の変更ファイルへ移動 (Alt+J)",
		"Go to the previous changed file":         "前の変更ファイルへ移動", "Go to the next changed file": "次の変更ファイルへ移動",
	},
	"es": {
		"Previous changed file": "Archivo modificado anterior", "Next changed file": "Archivo modificado siguiente",
		"Go to the previous changed file (Alt+K)": "Ir al archivo modificado anterior (Alt+K)",
		"Go to the next changed file (Alt+J)":     "Ir al archivo modificado siguiente (Alt+J)",
		"Go to the previous changed file":         "Ir al archivo modificado anterior", "Go to the next changed file": "Ir al archivo modificado siguiente",
	},
	"fr": {
		"Previous changed file": "Fichier modifié précédent", "Next changed file": "Fichier modifié suivant",
		"Go to the previous changed file (Alt+K)": "Aller au fichier modifié précédent (Alt+K)",
		"Go to the next changed file (Alt+J)":     "Aller au fichier modifié suivant (Alt+J)",
		"Go to the previous changed file":         "Aller au fichier modifié précédent", "Go to the next changed file": "Aller au fichier modifié suivant",
	},
}

func init() {
	catalogs := map[string]map[string]string{"zh-Hans": zhHans, "zh-Hant": zhHant, "ja": japanese, "es": spanish, "fr": french}
	for language, additions := range changeWorkflowTranslations {
		for english, translated := range additions {
			catalogs[language][english] = translated
		}
	}
}
