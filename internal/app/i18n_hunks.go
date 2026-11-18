package app

// One-click hunk actions share the additive-catalog pattern used by the patch
// workflows. Keeping safety explanations beside their labels also covers the
// dynamically selected disabled-reason strings.
var hunkActionTranslations = []struct {
	english, zhHans, zhHant, japanese, spanish, french string
}{
	{"Stage hunk", "暂存当前块", "暫存目前區塊", "ハンクをステージ", "Preparar bloque", "Indexer la section"},
	{"Unstage hunk", "取消暂存当前块", "取消暫存目前區塊", "ハンクのステージを解除", "Quitar bloque de la preparación", "Désindexer la section"},
	{"Discard hunk", "丢弃当前块", "捨棄目前區塊", "ハンクを破棄", "Descartar bloque", "Abandonner la section"},
	{"Previous hunk", "上一个块", "上一個區塊", "前のハンク", "Bloque anterior", "Section précédente"},
	{"Next hunk", "下一个块", "下一個區塊", "次のハンク", "Bloque siguiente", "Section suivante"},
	{"Hunk %d/%d", "块 %d/%d", "區塊 %d/%d", "ハンク %d/%d", "Bloque %d/%d", "Section %d/%d"},
	{"Stage only the current hunk (s), leaving other hunks unstaged", "仅暂存当前块 (s)，其他块保持未暂存", "僅暫存目前區塊 (s)，其他區塊保持未暫存", "現在のハンクだけをステージ (s) し、他のハンクは未ステージのままにします", "Preparar solo el bloque actual (s) y dejar los demás sin preparar", "Indexer uniquement la section actuelle (s), sans indexer les autres"},
	{"Unstage only the current hunk (s); leave it in the working tree", "仅取消暂存当前块 (s)，并保留在工作树中", "僅取消暫存目前區塊 (s)，並保留在工作樹中", "現在のハンクだけのステージを解除 (s) し、作業ツリーには残します", "Quitar solo el bloque actual de la preparación (s) y conservarlo en el árbol de trabajo", "Désindexer uniquement la section actuelle (s) en la conservant dans l’arbre de travail"},
	{"Discard the current hunk after confirmation (d); keep the index untouched", "确认后丢弃当前块 (d)，索引保持不变", "確認後捨棄目前區塊 (d)，索引保持不變", "確認後に現在のハンクを破棄 (d) し、インデックスは変更しません", "Descartar el bloque actual tras confirmarlo (d); el índice permanece intacto", "Abandonner la section actuelle après confirmation (d), sans modifier l’index"},
	{"Go to the previous diff hunk ([)", "转到上一个差异块 ([)", "轉到上一個差異區塊 ([)", "前の diff ハンクへ ([)", "Ir al bloque anterior del diff ([)", "Aller à la section de diff précédente ([)"},
	{"Go to the next diff hunk (])", "转到下一个差异块 (])", "轉到下一個差異區塊 (])", "次の diff ハンクへ (])", "Ir al bloque siguiente del diff (])", "Aller à la section de diff suivante (])"},
	{"Hunk action unavailable", "块操作不可用", "區塊操作不可用", "ハンク操作は利用できません", "Acción de bloque no disponible", "Action de section indisponible"},
	{"No current hunk", "没有当前块", "沒有目前區塊", "現在のハンクはありません", "No hay ningún bloque actual", "Aucune section actuelle"},
	{"Nothing to change", "没有可更改的内容", "沒有可變更的內容", "変更するものはありません", "No hay nada que cambiar", "Rien à modifier"},
	{"Discard Hunk", "丢弃块", "捨棄區塊", "ハンクを破棄", "Descartar bloque", "Abandonner la section"},
	{"Discard hunk %d of %d in %s?", "要丢弃第 %d/%d 个块（%s）吗？", "要捨棄第 %d/%d 個區塊（%s）嗎？", "%d/%d 番目のハンクを破棄しますか（%s）？", "¿Descartar el bloque %d de %d en %s?", "Abandonner la section %d sur %d de %s ?"},
	{"Only this hunk's working-tree changes are removed; the index and staged changes stay untouched. A recovery copy is kept for 7 days in Saved work.", "仅移除此块在工作树中的更改，索引和已暂存的更改保持不变。恢复副本会在 Saved work 中保留 7 天。", "僅移除此區塊在工作樹中的變更，索引與已暫存變更保持不變。復原副本會在 Saved work 中保留 7 天。", "このハンクの作業ツリー上の変更だけを削除し、インデックスとステージ済みの変更は保持します。回復コピーは Saved work に 7 日間保存されます。", "Solo se eliminan los cambios de este bloque en el árbol de trabajo; el índice y los cambios preparados permanecen intactos. Se conserva una copia de recuperación durante 7 días en Saved work.", "Seules les modifications de cette section dans l’arbre de travail sont supprimées ; l’index et les modifications indexées sont préservés. Une copie de récupération est conservée 7 jours dans Saved work."},
	{"Discarded hunk in %s", "已丢弃 %s 中的块", "已捨棄 %s 中的區塊", "%s のハンクを破棄しました", "Bloque descartado en %s", "Section abandonnée dans %s"},
	{"Stage or unstage the current diff hunk", "暂存或取消暂存当前差异块", "暫存或取消暫存目前差異區塊", "現在の diff ハンクをステージまたはステージ解除", "Preparar o quitar de la preparación el bloque actual del diff", "Indexer ou désindexer la section de diff actuelle"},
	{"Discard the current working-tree diff hunk after confirmation", "确认后丢弃当前工作树差异块", "確認後捨棄目前工作樹差異區塊", "確認後に現在の作業ツリー diff ハンクを破棄", "Descartar el bloque actual del diff del árbol de trabajo tras confirmarlo", "Abandonner la section de diff actuelle de l’arbre de travail après confirmation"},
	{"Choose a changed file first.", "请先选择一个已更改的文件。", "請先選擇一個已變更的檔案。", "先に変更されたファイルを選択してください。", "Elige primero un archivo modificado.", "Choisissez d’abord un fichier modifié."},
	{"Combined conflict diffs cannot be staged or discarded by hunk. Resolve the conflict or use a whole-file action.", "组合冲突差异不能按块暂存或丢弃。请解决冲突或使用整个文件操作。", "組合衝突差異無法按區塊暫存或捨棄。請解決衝突或使用整個檔案操作。", "結合された競合 diff はハンク単位でステージまたは破棄できません。競合を解決するか、ファイル全体の操作を使用してください。", "Los diffs combinados de conflictos no se pueden preparar ni descartar por bloques. Resuelve el conflicto o usa una acción de archivo completo.", "Les diffs de conflit combinés ne peuvent pas être indexés ou abandonnés par section. Résolvez le conflit ou utilisez une action sur le fichier entier."},
	{"Wait for this file's diff to load.", "请等待此文件的差异加载完成。", "請等待此檔案的差異載入完成。", "このファイルの diff の読み込みをお待ちください。", "Espera a que cargue el diff de este archivo.", "Attendez le chargement du diff de ce fichier."},
	{"The current diff could not be read. Refresh it before changing a hunk.", "无法读取当前差异。请刷新后再更改块。", "無法讀取目前差異。請重新整理後再變更區塊。", "現在の diff を読み取れませんでした。ハンクを変更する前に再読み込みしてください。", "No se pudo leer el diff actual. Actualízalo antes de cambiar un bloque.", "Impossible de lire le diff actuel. Actualisez-le avant de modifier une section."},
	{"A staged deletion can only be unstaged as a whole file.", "已暂存的删除只能按整个文件取消暂存。", "已暫存的刪除只能按整個檔案取消暫存。", "ステージされた削除はファイル全体でのみステージ解除できます。", "Una eliminación preparada solo se puede quitar de la preparación como archivo completo.", "Une suppression indexée ne peut être désindexée que pour le fichier entier."},
	{"The current file has no textual diff hunks.", "当前文件没有文本差异块。", "目前檔案沒有文字差異區塊。", "現在のファイルにはテキスト diff ハンクがありません。", "El archivo actual no tiene bloques de diff de texto.", "Le fichier actuel ne contient aucune section de diff textuelle."},
	{"Binary changes cannot be divided into hunks. Use the whole-file actions instead.", "二进制更改不能拆分为块。请改用整个文件操作。", "二進位變更無法拆分為區塊。請改用整個檔案操作。", "バイナリ変更はハンクに分割できません。ファイル全体の操作を使用してください。", "Los cambios binarios no se pueden dividir en bloques. Usa las acciones de archivo completo.", "Les modifications binaires ne peuvent pas être divisées en sections. Utilisez les actions sur le fichier entier."},
	{"This diff has no content hunks to change.", "此差异没有可更改的内容块。", "此差異沒有可變更的內容區塊。", "この diff には変更できるコンテンツハンクがありません。", "Este diff no tiene bloques de contenido que cambiar.", "Ce diff ne contient aucune section de contenu à modifier."},
	{"Mode-only changes cannot be divided into hunks. Use the whole-file actions instead.", "仅模式更改不能拆分为块。请改用整个文件操作。", "僅模式變更無法拆分為區塊。請改用整個檔案操作。", "モードのみの変更はハンクに分割できません。ファイル全体の操作を使用してください。", "Los cambios solo de modo no se pueden dividir en bloques. Usa las acciones de archivo completo.", "Les modifications de mode seul ne peuvent pas être divisées en sections. Utilisez les actions sur le fichier entier."},
}

func init() {
	for _, entry := range hunkActionTranslations {
		translations["zh-Hans"][entry.english] = entry.zhHans
		translations["zh-Hant"][entry.english] = entry.zhHant
		translations["ja"][entry.english] = entry.japanese
		translations["es"][entry.english] = entry.spanish
		translations["fr"][entry.english] = entry.french
	}
}
