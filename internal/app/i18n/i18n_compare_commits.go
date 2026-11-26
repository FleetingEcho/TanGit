package i18n

// The Compare page's commit view: the commits only one side of a comparison
// has, the merge base, and the Branch changes / Exact difference choice.
var compareCommitsTranslations = []struct {
	English, ZhHans, ZhHant, Japanese, Spanish, French string
}{
	{"Compare as", "对比方式", "比較方式", "比較方法", "Comparar como", "Mode de comparaison"},
	{"Branch changes", "分支改动", "分支變更", "ブランチの変更", "Cambios de la rama", "Changements de la branche"},
	{"Exact difference", "精确差异", "精確差異", "厳密な差分", "Diferencia exacta", "Différence exacte"},
	{"Compare the common ancestor with the To ref to see what changed on that side since the refs diverged.",
		"比较共同祖先与目标引用，查看目标引用自分叉以来的改动。",
		"比較共同祖先與目標引用，查看目標引用自分岔以來的變更。",
		"共通の祖先と比較先の参照を比較し、分岐してから比較先で何が変わったかを確認します。",
		"Compara el ancestro común con la referencia de destino para ver qué cambió en ella desde la divergencia.",
		"Compare l’ancêtre commun à la référence de destination pour voir ce qui a changé de ce côté depuis la divergence."},
	{"Compare the current contents of the From and To refs, including changes made on either side.",
		"直接比较起始引用和目标引用的当前内容，两边的改动都会反映在差异中。",
		"直接比較起始引用和目標引用目前的內容，兩邊的變更都會反映在差異中。",
		"比較元と比較先の参照の現在の内容を比較します。両方の変更が差分に反映されます。",
		"Compara el contenido actual de las referencias de origen y de destino, incluidos los cambios de ambos lados.",
		"Compare le contenu actuel des références de départ et de destination, en tenant compte des changements des deux côtés."},
	{"merge base %s", "合并基点 %s", "合併基點 %s", "マージベース %s", "base de fusión %s", "base de fusion %s"},
	{"The latest commit %s and %s have in common", "%[1]s 与 %[2]s 共有的最新提交", "%[1]s 與 %[2]s 共有的最新提交", "%[1]s と %[2]s に共通する最新のコミット", "El último commit que %[1]s y %[2]s tienen en común", "Le dernier commit commun à %[1]s et %[2]s"},
	{"no common history", "没有共同历史", "沒有共同歷史", "共通の履歴なし", "sin historial común", "aucun historique commun"},
	{"These refs share no commits; Branch changes compares them directly.", "这两个引用没有共同的提交；“分支改动”会直接对比它们。", "這兩個引用沒有共同的提交；「分支變更」會直接比較它們。", "これらの参照には共通のコミットがありません。「ブランチの変更」は両者を直接比較します。", "Estas referencias no comparten commits; «Cambios de la rama» las compara directamente.", "Ces références ne partagent aucun commit ; « Changements de la branche » les compare directement."},
	{"%d files changed", "%d 个文件有改动", "%d 個檔案有變更", "変更されたファイル %d 個", "%d archivos cambiados", "%d fichiers modifiés"},
	{"Only in %s", "仅在 %s", "僅在 %s", "%s のみ", "Solo en %s", "Uniquement dans %s"},
	{"No commits only in %s.", "%s 中没有独有的提交。", "%s 中沒有獨有的提交。", "%s だけにあるコミットはありません。", "No hay commits solo en %s.", "Aucun commit n’est propre à %s."},
	{"Showing the newest %d commits.", "仅显示最新的 %d 个提交。", "僅顯示最新的 %d 個提交。", "最新の %d 件のコミットのみ表示しています。", "Se muestran solo los %d commits más recientes.", "Seuls les %d commits les plus récents sont affichés."},
}

func init() {
	for _, entry := range compareCommitsTranslations {
		Translations["zh-Hans"][entry.English] = entry.ZhHans
		Translations["zh-Hant"][entry.English] = entry.ZhHant
		Translations["ja"][entry.English] = entry.Japanese
		Translations["es"][entry.English] = entry.Spanish
		Translations["fr"][entry.English] = entry.French
	}
}
