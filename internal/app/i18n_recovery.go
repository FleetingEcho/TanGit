package app

var recoveryTranslations = map[string]map[string]string{
	"zh-Hans": {
		"Recovery & checkpoints": "恢复与检查点", "Checkpoint name": "检查点名称", "Name a checkpoint": "命名一个检查点",
		"Create checkpoint": "创建检查点", "Save all current work without changing it": "保存所有当前工作，但不更改其内容", "Created checkpoint %s": "已创建检查点 %s",
		"Recovery copies appear here after a discard. Named checkpoints stay until deleted.": "丢弃后，恢复副本会显示在这里。命名检查点会保留到删除为止。",
		"Recovery": "恢复", "Checkpoint": "检查点", "Restore…": "恢复…", "Restore": "恢复", "Restore saved work while keeping this entry": "恢复已保存的工作并保留此条目",
		"Delete this recovery entry…": "删除此恢复条目…", "Restore Saved Work": "恢复已保存的工作", "Restore %s?": "恢复 %s？",
		"New changes to the same paths stop the restore before anything is overwritten. The recovery entry is kept.": "如果相同路径存在新更改，恢复会在覆盖任何内容之前停止。恢复条目会保留。", "Restored %s": "已恢复 %s",
		"Delete Saved Work": "删除已保存的工作", "Delete %s?": "删除 %s？", "The entry will no longer be discoverable in TanGit. This cannot be undone from the app.": "此条目将无法再在 TanGit 中找到。此操作无法从应用中撤销。", "Deleted %s": "已删除 %s",
		"A recovery copy is kept for 7 days in Saved work. The current changes will be removed.":       "恢复副本会在“已保存工作”中保留 7 天。当前更改将被移除。",
		"Staged and unstaged changes are removed. One recovery copy is kept for 7 days in Saved work.": "已暂存和未暂存的更改将被移除。一个恢复副本会在“已保存工作”中保留 7 天。",
	},
	"zh-Hant": {
		"Recovery & checkpoints": "復原與檢查點", "Checkpoint name": "檢查點名稱", "Name a checkpoint": "命名檢查點",
		"Create checkpoint": "建立檢查點", "Save all current work without changing it": "保存所有目前工作而不加以變更", "Created checkpoint %s": "已建立檢查點 %s",
		"Recovery copies appear here after a discard. Named checkpoints stay until deleted.": "捨棄後，復原副本會顯示在這裡。命名檢查點會保留到刪除為止。",
		"Recovery": "復原", "Checkpoint": "檢查點", "Restore…": "還原…", "Restore": "還原", "Restore saved work while keeping this entry": "還原已保存的工作並保留此項目",
		"Delete this recovery entry…": "刪除此復原項目…", "Restore Saved Work": "還原已保存的工作", "Restore %s?": "還原 %s？",
		"New changes to the same paths stop the restore before anything is overwritten. The recovery entry is kept.": "若相同路徑有新變更，還原會在覆寫任何內容前停止。復原項目會保留。", "Restored %s": "已還原 %s",
		"Delete Saved Work": "刪除已保存的工作", "Delete %s?": "刪除 %s？", "The entry will no longer be discoverable in TanGit. This cannot be undone from the app.": "此項目將無法再在 TanGit 中找到。此操作無法從應用程式復原。", "Deleted %s": "已刪除 %s",
		"A recovery copy is kept for 7 days in Saved work. The current changes will be removed.":       "復原副本會在「已保存工作」中保留 7 天。目前變更將被移除。",
		"Staged and unstaged changes are removed. One recovery copy is kept for 7 days in Saved work.": "已暫存及未暫存的變更將被移除。一個復原副本會在「已保存工作」中保留 7 天。",
	},
	"ja": {
		"Recovery & checkpoints": "復元とチェックポイント", "Checkpoint name": "チェックポイント名", "Name a checkpoint": "チェックポイント名を入力",
		"Create checkpoint": "チェックポイントを作成", "Save all current work without changing it": "現在の作業を変更せずにすべて保存", "Created checkpoint %s": "チェックポイント %s を作成しました",
		"Recovery copies appear here after a discard. Named checkpoints stay until deleted.": "破棄後の復元コピーはここに表示されます。名前付きチェックポイントは削除するまで保持されます。",
		"Recovery": "復元", "Checkpoint": "チェックポイント", "Restore…": "復元…", "Restore": "復元", "Restore saved work while keeping this entry": "この項目を残したまま保存した作業を復元",
		"Delete this recovery entry…": "この復元項目を削除…", "Restore Saved Work": "保存した作業の復元", "Restore %s?": "%s を復元しますか？",
		"New changes to the same paths stop the restore before anything is overwritten. The recovery entry is kept.": "同じパスに新しい変更がある場合、上書き前に復元を中止します。復元項目は保持されます。", "Restored %s": "%s を復元しました",
		"Delete Saved Work": "保存した作業の削除", "Delete %s?": "%s を削除しますか？", "The entry will no longer be discoverable in TanGit. This cannot be undone from the app.": "この項目は TanGit に表示されなくなります。アプリから元に戻すことはできません。", "Deleted %s": "%s を削除しました",
		"A recovery copy is kept for 7 days in Saved work. The current changes will be removed.":       "復元コピーは「保存した作業」に 7 日間保持されます。現在の変更は削除されます。",
		"Staged and unstaged changes are removed. One recovery copy is kept for 7 days in Saved work.": "ステージ済みと未ステージの変更を削除します。復元コピーは「保存した作業」に 7 日間保持されます。",
	},
	"es": {
		"Recovery & checkpoints": "Recuperación y puntos de control", "Checkpoint name": "Nombre del punto de control", "Name a checkpoint": "Nombrar un punto de control",
		"Create checkpoint": "Crear punto de control", "Save all current work without changing it": "Guardar todo el trabajo actual sin modificarlo", "Created checkpoint %s": "Punto de control %s creado",
		"Recovery copies appear here after a discard. Named checkpoints stay until deleted.": "Las copias de recuperación aparecen aquí tras descartar. Los puntos de control con nombre se conservan hasta eliminarlos.",
		"Recovery": "Recuperación", "Checkpoint": "Punto de control", "Restore…": "Restaurar…", "Restore": "Restaurar", "Restore saved work while keeping this entry": "Restaurar el trabajo guardado conservando esta entrada",
		"Delete this recovery entry…": "Eliminar esta entrada de recuperación…", "Restore Saved Work": "Restaurar trabajo guardado", "Restore %s?": "¿Restaurar %s?",
		"New changes to the same paths stop the restore before anything is overwritten. The recovery entry is kept.": "Los cambios nuevos en las mismas rutas detienen la restauración antes de sobrescribir nada. La entrada se conserva.", "Restored %s": "%s restaurado",
		"Delete Saved Work": "Eliminar trabajo guardado", "Delete %s?": "¿Eliminar %s?", "The entry will no longer be discoverable in TanGit. This cannot be undone from the app.": "La entrada dejará de estar disponible en TanGit. No se puede deshacer desde la aplicación.", "Deleted %s": "%s eliminado",
		"A recovery copy is kept for 7 days in Saved work. The current changes will be removed.":       "Se conserva una copia de recuperación durante 7 días en Trabajo guardado. Los cambios actuales se eliminarán.",
		"Staged and unstaged changes are removed. One recovery copy is kept for 7 days in Saved work.": "Se eliminan los cambios preparados y sin preparar. Una copia se conserva 7 días en Trabajo guardado.",
	},
	"fr": {
		"Recovery & checkpoints": "Récupération et points de contrôle", "Checkpoint name": "Nom du point de contrôle", "Name a checkpoint": "Nommer un point de contrôle",
		"Create checkpoint": "Créer un point de contrôle", "Save all current work without changing it": "Enregistrer tout le travail actuel sans le modifier", "Created checkpoint %s": "Point de contrôle %s créé",
		"Recovery copies appear here after a discard. Named checkpoints stay until deleted.": "Les copies de récupération apparaissent ici après un abandon. Les points de contrôle nommés restent jusqu’à leur suppression.",
		"Recovery": "Récupération", "Checkpoint": "Point de contrôle", "Restore…": "Restaurer…", "Restore": "Restaurer", "Restore saved work while keeping this entry": "Restaurer le travail enregistré en conservant cette entrée",
		"Delete this recovery entry…": "Supprimer cette entrée de récupération…", "Restore Saved Work": "Restaurer le travail enregistré", "Restore %s?": "Restaurer %s ?",
		"New changes to the same paths stop the restore before anything is overwritten. The recovery entry is kept.": "De nouvelles modifications sur les mêmes chemins arrêtent la restauration avant tout écrasement. L’entrée est conservée.", "Restored %s": "%s restauré",
		"Delete Saved Work": "Supprimer le travail enregistré", "Delete %s?": "Supprimer %s ?", "The entry will no longer be discoverable in TanGit. This cannot be undone from the app.": "Cette entrée ne sera plus accessible dans TanGit. Cette action est irréversible depuis l’application.", "Deleted %s": "%s supprimé",
		"A recovery copy is kept for 7 days in Saved work. The current changes will be removed.":       "Une copie de récupération est conservée 7 jours dans Travail enregistré. Les modifications actuelles seront supprimées.",
		"Staged and unstaged changes are removed. One recovery copy is kept for 7 days in Saved work.": "Les modifications indexées et non indexées sont supprimées. Une copie est conservée 7 jours dans Travail enregistré.",
	},
}

func init() {
	catalogs := map[string]map[string]string{"zh-Hans": zhHans, "zh-Hant": zhHant, "ja": japanese, "es": spanish, "fr": french}
	for language, additions := range recoveryTranslations {
		for english, translated := range additions {
			catalogs[language][english] = translated
		}
	}
}
