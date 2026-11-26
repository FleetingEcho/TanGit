package i18n

func init() {
	labels := [...]string{
		"Search code changes",
		"Find text added or removed in commit history",
		"Code change search requires the History view",
		"Text to find in changes",
		"Occurrence count",
		"Changed lines",
		"Search filters",
		"Whole repository",
		"Apply filters",
		"Previous results",
		`Find added or removed lines with # changed:"<code>"`,
		"Find commits where the text became more or less frequent in a file.",
		"Regular expression",
		"File or folder",
		"Branch, tag or range",
		"HEAD or v1.0..v2.0",
		"Search stopped. Results are partial.",
		"Search timed out. Results are partial.",
		"No matches in this batch.",
		"%d checked · %d found",
		"Stop search",
		"Previous batch",
		"Continue into older history",
		"Resume search",
		"Search",
	}
	translations := map[string][len(labels)]string{
		"zh-Hans": {
			"搜索代码变化", "查找提交历史中新增或删除的文本", "搜索代码变化需要启用历史视图",
			"要在改动中查找的文本", "出现次数变化", "改动行", "筛选条件", "整个仓库", "应用筛选", "上次搜索结果", `用 # changed:"<code>" 查找新增或删除的行`, "查找文本在文件中出现次数增加或减少的提交。",
			"正则表达式", "文件或目录", "分支、标签或范围", "HEAD 或 v1.0..v2.0",
			"搜索已停止，结果可能不完整。", "搜索超时，结果可能不完整。", "本批次没有匹配项。", "已检查 %d 个提交 · %d 个匹配",
			"停止搜索", "上一批", "继续搜索更早的历史", "继续搜索", "搜索",
		},
		"zh-Hant": {
			"搜尋程式碼變更", "找出提交歷史中新增或刪除的文字", "搜尋程式碼變更需要啟用歷史檢視",
			"要在變更中搜尋的文字", "出現次數變化", "變更行", "篩選條件", "整個儲存庫", "套用篩選", "上次搜尋結果", `用 # changed:"<code>" 找出新增或刪除的行`, "找出文字在檔案中出現次數增加或減少的提交。",
			"正規表示式", "檔案或資料夾", "分支、標籤或範圍", "HEAD 或 v1.0..v2.0",
			"搜尋已停止，結果可能不完整。", "搜尋逾時，結果可能不完整。", "本批次沒有符合項目。", "已檢查 %d 個提交 · %d 個符合項目",
			"停止搜尋", "上一批", "繼續搜尋更早的歷史", "繼續搜尋", "搜尋",
		},
		"ja": {
			"コードの変更を検索", "コミット履歴で追加・削除されたテキストを検索", "コードの変更を検索するには履歴ビューが必要です",
			"変更箇所で探すテキスト", "出現回数の変化", "変更行", "検索フィルター", "リポジトリ全体", "フィルターを適用", "前回の検索結果", `# changed:"<code>" で追加・削除された行を検索`, "ファイル内でテキストの出現回数が増減したコミットを検索します。",
			"正規表現", "ファイルまたはフォルダー", "ブランチ、タグ、または範囲", "HEAD または v1.0..v2.0",
			"検索を停止しました。結果は一部のみです。", "検索がタイムアウトしました。結果は一部のみです。", "この範囲に一致する結果はありません。", "%d 件確認 · %d 件一致",
			"検索を停止", "前の範囲", "さらに古い履歴を検索", "検索を再開", "検索",
		},
		"es": {
			"Buscar cambios de código", "Busca texto añadido o eliminado en el historial", "La búsqueda de cambios requiere la vista Historial",
			"Texto que buscar en los cambios", "Número de apariciones", "Líneas cambiadas", "Filtros de búsqueda", "Todo el repositorio", "Aplicar filtros", "Resultados anteriores", `Busca líneas añadidas o eliminadas con # changed:"<code>"`, "Busca commits donde haya aumentado o disminuido el número de apariciones del texto en un archivo.",
			"Expresión regular", "Archivo o carpeta", "Rama, etiqueta o rango", "HEAD o v1.0..v2.0",
			"Búsqueda detenida. Los resultados son parciales.", "Se agotó el tiempo de búsqueda. Los resultados son parciales.", "Sin coincidencias en este lote.", "%d revisados · %d coincidencias",
			"Detener búsqueda", "Lote anterior", "Buscar en el historial anterior", "Reanudar búsqueda", "Buscar",
		},
		"fr": {
			"Rechercher dans les changements de code", "Rechercher du texte ajouté ou supprimé dans l’historique", "La recherche de changements nécessite la vue Historique",
			"Texte à rechercher dans les changements", "Nombre d’occurrences", "Lignes modifiées", "Filtres de recherche", "Dépôt entier", "Appliquer les filtres", "Résultats précédents", `Rechercher les lignes ajoutées ou supprimées avec # changed:"<code>"`, "Trouver les commits qui augmentent ou réduisent le nombre d’occurrences du texte dans un fichier.",
			"Expression régulière", "Fichier ou dossier", "Branche, tag ou plage", "HEAD ou v1.0..v2.0",
			"Recherche arrêtée. Les résultats sont partiels.", "Délai de recherche dépassé. Les résultats sont partiels.", "Aucun résultat dans ce lot.", "%d examinés · %d résultats",
			"Arrêter la recherche", "Lot précédent", "Rechercher plus loin dans l’historique", "Reprendre la recherche", "Rechercher",
		},
	}
	for language, entries := range translations {
		for i, label := range labels {
			if _, exists := Translations[language][label]; !exists {
				Translations[language][label] = entries[i]
			}
		}
	}
}
