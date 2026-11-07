package app

// Review-workflow strings are kept together because the same controls appear
// in the file list, context menu, keyboard help and filter row.
var reviewTranslations = map[string]map[string]string{
	"zh-Hans": {
		"Mark the chosen file reviewed or unreviewed":      "将所选文件标记为已审阅或未审阅",
		"Go to the next unreviewed file":                   "转到下一个未审阅文件",
		"Next unreviewed":                                  "下一个未审阅",
		"Go to the next unreviewed file (Shift+R)":         "转到下一个未审阅文件 (Shift+R)",
		"%d/%d reviewed":                                   "已审阅 %d/%d",
		"Changed content automatically becomes unreviewed": "内容更改后会自动变为未审阅",
		"Review marks are only a status in TanGit; they do not change Git information.": "审阅标记仅是 TanGit 中的状态；不会更改 Git 信息。",
		"Toggle whether this file's current content has been reviewed (R)":              "切换是否已审阅此文件的当前内容 (R)",
		"Reviewed":        "已审阅",
		"Mark reviewed":   "标记为已审阅",
		"Mark unreviewed": "标记为未审阅",
		"Show only files whose current content still needs review": "仅显示当前内容仍需审阅的文件",
		"Unreviewed": "未审阅",
	},
	"zh-Hant": {
		"Mark the chosen file reviewed or unreviewed":      "將所選檔案標記為已審閱或未審閱",
		"Go to the next unreviewed file":                   "前往下一個未審閱檔案",
		"Next unreviewed":                                  "下一個未審閱",
		"Go to the next unreviewed file (Shift+R)":         "前往下一個未審閱檔案 (Shift+R)",
		"%d/%d reviewed":                                   "已審閱 %d/%d",
		"Changed content automatically becomes unreviewed": "內容更改後會自動變為未審閱",
		"Review marks are only a status in TanGit; they do not change Git information.": "審閱標記僅是 TanGit 中的狀態；不會更改 Git 資訊。",
		"Toggle whether this file's current content has been reviewed (R)":              "切換是否已審閱此檔案的目前內容 (R)",
		"Reviewed":        "已審閱",
		"Mark reviewed":   "標記為已審閱",
		"Mark unreviewed": "標記為未審閱",
		"Show only files whose current content still needs review": "僅顯示目前內容仍需審閱的檔案",
		"Unreviewed": "未審閱",
	},
	"ja": {
		"Mark the chosen file reviewed or unreviewed":      "選択したファイルのレビュー済み状態を切り替える",
		"Go to the next unreviewed file":                   "次の未レビューファイルへ移動",
		"Next unreviewed":                                  "次の未レビュー",
		"Go to the next unreviewed file (Shift+R)":         "次の未レビューファイルへ移動 (Shift+R)",
		"%d/%d reviewed":                                   "%d/%d レビュー済み",
		"Changed content automatically becomes unreviewed": "変更された内容は自動的に未レビューになります",
		"Review marks are only a status in TanGit; they do not change Git information.": "レビューマークは TanGit 内だけのステータスであり、Git の情報は変更されません。",
		"Toggle whether this file's current content has been reviewed (R)":              "このファイルの現在の内容のレビュー済み状態を切り替え (R)",
		"Reviewed":        "レビュー済み",
		"Mark reviewed":   "レビュー済みにする",
		"Mark unreviewed": "未レビューにする",
		"Show only files whose current content still needs review": "現在の内容がまだレビューされていないファイルのみ表示",
		"Unreviewed": "未レビュー",
	},
	"es": {
		"Mark the chosen file reviewed or unreviewed":      "Marcar el archivo elegido como revisado o no revisado",
		"Go to the next unreviewed file":                   "Ir al siguiente archivo no revisado",
		"Next unreviewed":                                  "Siguiente no revisado",
		"Go to the next unreviewed file (Shift+R)":         "Ir al siguiente archivo no revisado (Shift+R)",
		"%d/%d reviewed":                                   "%d/%d revisados",
		"Changed content automatically becomes unreviewed": "El contenido modificado pasa automáticamente a no revisado",
		"Review marks are only a status in TanGit; they do not change Git information.": "Las marcas de revisión son solo un estado de TanGit; no cambian la información de Git.",
		"Toggle whether this file's current content has been reviewed (R)":              "Alternar si se revisó el contenido actual de este archivo (R)",
		"Reviewed":        "Revisado",
		"Mark reviewed":   "Marcar como revisado",
		"Mark unreviewed": "Marcar como no revisado",
		"Show only files whose current content still needs review": "Mostrar solo archivos cuyo contenido actual necesita revisión",
		"Unreviewed": "No revisados",
	},
	"fr": {
		"Mark the chosen file reviewed or unreviewed":      "Marquer le fichier choisi comme relu ou non relu",
		"Go to the next unreviewed file":                   "Aller au prochain fichier non relu",
		"Next unreviewed":                                  "Prochain non relu",
		"Go to the next unreviewed file (Shift+R)":         "Aller au prochain fichier non relu (Shift+R)",
		"%d/%d reviewed":                                   "%d/%d relus",
		"Changed content automatically becomes unreviewed": "Le contenu modifié redevient automatiquement non relu",
		"Review marks are only a status in TanGit; they do not change Git information.": "Les marques de relecture sont uniquement un état dans TanGit ; elles ne modifient pas les informations Git.",
		"Toggle whether this file's current content has been reviewed (R)":              "Basculer la relecture du contenu actuel de ce fichier (R)",
		"Reviewed":        "Relu",
		"Mark reviewed":   "Marquer comme relu",
		"Mark unreviewed": "Marquer comme non relu",
		"Show only files whose current content still needs review": "Afficher uniquement les fichiers dont le contenu actuel reste à relire",
		"Unreviewed": "Non relus",
	},
}

func init() {
	catalogs := map[string]map[string]string{
		"zh-Hans": zhHans,
		"zh-Hant": zhHant,
		"ja":      japanese,
		"es":      spanish,
		"fr":      french,
	}
	for language, additions := range reviewTranslations {
		for english, translated := range additions {
			catalogs[language][english] = translated
		}
	}
}
