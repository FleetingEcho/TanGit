package i18n

// Action-progress text separates reading recovery preparation from the
// accepted, durable update of staged files and the repository's own caches.
var fileActionTranslations = map[string]map[string]string{
	"zh-Hans": {
		"Checking recovery…":                           "正在检查恢复数据…",
		"Verifying saved recovery data before staging": "在暂存前核验已保存的恢复数据",
		"Updating file list…":                          "正在更新文件列表…",
		"Saving changes and refreshing the file list":  "正在保存更改并刷新文件列表",
	},
	"zh-Hant": {
		"Checking recovery…":                           "正在檢查復原資料…",
		"Verifying saved recovery data before staging": "在暫存前驗證儲存的復原資料",
		"Updating file list…":                          "正在更新檔案清單…",
		"Saving changes and refreshing the file list":  "正在儲存變更並整理檔案清單",
	},
	"ja": {
		"Checking recovery…":                           "復旧データを確認中…",
		"Verifying saved recovery data before staging": "ステージ前に保存済み復旧データを検証",
		"Updating file list…":                          "ファイル一覧を更新中…",
		"Saving changes and refreshing the file list":  "変更を保存してファイル一覧を更新しています",
	},
	"es": {
		"Checking recovery…":                           "Comprobando recuperación…",
		"Verifying saved recovery data before staging": "Verificando los datos de recuperación guardados antes de preparar",
		"Updating file list…":                          "Actualizando la lista de archivos…",
		"Saving changes and refreshing the file list":  "Guardando cambios y actualizando la lista de archivos",
	},
	"fr": {
		"Checking recovery…":                           "Vérification de la récupération…",
		"Verifying saved recovery data before staging": "Vérification des données de récupération enregistrées avant l’indexation",
		"Updating file list…":                          "Mise à jour de la liste des fichiers…",
		"Saving changes and refreshing the file list":  "Enregistrement des modifications et actualisation de la liste des fichiers",
	},
}

func init() {
	for language, entries := range fileActionTranslations {
		for english, translated := range entries {
			Translations[language][english] = translated
		}
	}
}
