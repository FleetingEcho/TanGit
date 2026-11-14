package app

// Dynamic confirmation titles cannot be found by the static tr/trf source
// audit. Keep these titles here and cover them with TestNewConfirmationTitlesAreLocalized.
var missingConfirmationTranslations = map[string]map[string]string{
	"zh-Hans": {
		"Discard Saved File": "丢弃已保存文件",
		"Mixed Reset":        "混合重置",

		"Edit Commit":       "编辑提交",
		"Checkout Commit":   "检出提交",
		"Push All Tags":     "推送所有标签",
		"Delete Remote Tag": "删除远程标签",

		"Mark Bisect Commit":    "标记二分查找提交",
		"Mark commit":           "标记提交",
		"Reset Bisect":          "重置二分查找",
		"Apply Mailbox Patch":   "应用邮件补丁",
		"Apply patch":           "应用补丁",
		"Open Pull Request":     "打开拉取请求",
		"Delete Stale Branches": "删除过时分支",
	},
	"zh-Hant": {
		"Discard Saved File": "捨棄已儲存檔案",
		"Mixed Reset":        "混合重設",

		"Edit Commit":       "編輯提交",
		"Checkout Commit":   "檢出提交",
		"Push All Tags":     "推送所有標籤",
		"Delete Remote Tag": "刪除遠端標籤",

		"Mark Bisect Commit":    "標記二分搜尋提交",
		"Mark commit":           "標記提交",
		"Reset Bisect":          "重設二分搜尋",
		"Apply Mailbox Patch":   "套用郵件修補程式",
		"Apply patch":           "套用修補",
		"Open Pull Request":     "開啟拉取請求",
		"Delete Stale Branches": "刪除過時分支",
	},
	"ja": {
		"Discard Saved File": "保存済みファイルを破棄",
		"Mixed Reset":        "混合リセット",

		"Edit Commit":       "コミットを編集",
		"Checkout Commit":   "コミットをチェックアウト",
		"Push All Tags":     "すべてのタグをプッシュ",
		"Delete Remote Tag": "リモートタグを削除",

		"Mark Bisect Commit":    "バイセクトの対象コミットをマーク",
		"Mark commit":           "コミットをマーク",
		"Reset Bisect":          "バイセクトをリセット",
		"Apply Mailbox Patch":   "メールボックスパッチを適用",
		"Apply patch":           "パッチを適用",
		"Open Pull Request":     "プルリクエストを開く",
		"Delete Stale Branches": "不要なブランチを削除",
	},
	"es": {
		"Discard Saved File": "Descartar archivo guardado",
		"Mixed Reset":        "Restablecimiento mixto",

		"Edit Commit":       "Editar commit",
		"Checkout Commit":   "Extraer commit",
		"Push All Tags":     "Enviar todas las etiquetas",
		"Delete Remote Tag": "Eliminar etiqueta remota",

		"Mark Bisect Commit":    "Marcar commit para búsqueda binaria",
		"Mark commit":           "Marcar commit",
		"Reset Bisect":          "Restablecer búsqueda binaria",
		"Apply Mailbox Patch":   "Aplicar parche de buzón",
		"Apply patch":           "Aplicar parche",
		"Open Pull Request":     "Abrir pull request",
		"Delete Stale Branches": "Eliminar ramas obsoletas",
	},
	"fr": {
		"Discard Saved File": "Abandonner le fichier enregistré",
		"Mixed Reset":        "Réinitialisation mixte",

		"Edit Commit":       "Modifier le commit",
		"Checkout Commit":   "Extraire le commit",
		"Push All Tags":     "Pousser tous les tags",
		"Delete Remote Tag": "Supprimer le tag distant",

		"Mark Bisect Commit":    "Marquer le commit pour la bissection",
		"Mark commit":           "Marquer le commit",
		"Reset Bisect":          "Réinitialiser la bissection",
		"Apply Mailbox Patch":   "Appliquer le patch de boîte aux lettres",
		"Apply patch":           "Appliquer le patch",
		"Open Pull Request":     "Ouvrir la pull request",
		"Delete Stale Branches": "Supprimer les branches obsolètes",
	},
}

func init() {
	for language, entries := range missingConfirmationTranslations {
		for english, translated := range entries {
			translations[language][english] = translated
		}
	}
}
