package i18n

// The visual rebase workspace uses the same additive catalog pattern as the
// other feature modules. Keeping the labels and their safety explanations in
// one table also covers dynamically selected step statuses and action labels.
var rebaseWorkspaceTranslations = []struct {
	English, ZhHans, ZhHant, Japanese, Spanish, French string
}{
	{"Back to Rebase TODO", "返回 Rebase TODO", "返回 Rebase TODO", "Rebase TODO に戻る", "Volver a Rebase TODO", "Retour au TODO de rebasage"},
	{"Return to the real rebase steps after resolving conflicts", "解决冲突后返回真实的 rebase 步骤", "解決衝突後返回真實的 rebase 步驟", "競合の解決後に実際のリベース手順へ戻る", "Volver a los pasos reales del rebase tras resolver los conflictos", "Revenir aux véritables étapes du rebasage après résolution des conflits"},
	{"Only commits above marked base %s are replayed with git rebase --onto %s %s.", "仅重放标记基点 %s 之后的提交，命令为 git rebase --onto %s %s。", "僅重放標記基點 %s 之後的提交，命令為 git rebase --onto %s %s。", "マークしたベース %s より後のコミットだけを git rebase --onto %s %s で再生します。", "Solo los commits posteriores a la base marcada %s se reproducen con git rebase --onto %s %s.", "Seuls les commits situés après la base marquée %s sont rejoués avec git rebase --onto %s %s."},
	{"Mark as base commit for rebase", "标记为 rebase 基点提交", "標記為 rebase 基點提交", "リベースのベースコミットとしてマーク", "Marcar como commit base para el rebase", "Marquer comme commit de base pour le rebasage"},
	{"Cancel marked base commit", "取消标记的基点提交", "取消標記的基點提交", "マークしたベースコミットを解除", "Cancelar el commit base marcado", "Annuler le commit de base marqué"},
	{"Open Rebase TODO workspace", "打开 Rebase TODO 工作区", "開啟 Rebase TODO 工作區", "Rebase TODO ワークスペースを開く", "Abrir el espacio de trabajo Rebase TODO", "Ouvrir l’espace de travail TODO de rebasage"},
	{"Rebase base", "Rebase 基点", "Rebase 基點", "リベースベース", "Base del rebase", "Base de rebasage"},
	{"Start or inspect a real Git rebase plan", "开始或检查真实的 Git rebase 计划", "開始或檢查真實的 Git rebase 計畫", "実際の Git リベース計画を開始または確認", "Iniciar o inspeccionar un plan de rebase real de Git", "Démarrer ou examiner un véritable plan de rebasage Git"},
	{"draft", "草稿", "草稿", "下書き", "borrador", "brouillon"},
	{"updates", "更新", "更新", "更新", "actualiza", "met à jour"},
	{"Git updates this ref when the rebase finishes", "Git 会在 rebase 完成时更新此引用", "Git 會在 rebase 完成時更新此參照", "Git はリベース完了時にこの参照を更新します", "Git actualiza esta referencia al terminar el rebase", "Git met cette référence à jour à la fin du rebasage"},
	{"Resolve conflicts", "解决冲突", "解決衝突", "競合を解決", "Resolver conflictos", "Résoudre les conflits"},
	{"Open the existing conflict resolver in Changes", "打开 Changes 中现有的冲突解决器", "開啟 Changes 中現有的衝突解決器", "Changes の既存の競合解決画面を開く", "Abrir el solucionador de conflictos existente en Changes", "Ouvrir le résolveur de conflits existant dans Changes"},
	{"Continue through the Operation driver", "通过 Operation driver 继续", "透過 Operation driver 繼續", "Operation driver 経由で続行", "Continuar mediante el controlador de operaciones", "Continuer via le pilote d’opération"},
	{"Skip the current Git todo step", "跳过当前 Git todo 步骤", "跳過目前 Git todo 步驟", "現在の Git todo ステップをスキップ", "Omitir el paso actual del todo de Git", "Ignorer l’étape actuelle du TODO Git"},
	{"Abort through the Operation driver", "通过 Operation driver 中止", "透過 Operation driver 中止", "Operation driver 経由で中止", "Abortar mediante el controlador de operaciones", "Abandonner via le pilote d’opération"},
	{"Rebase TODO", "Rebase TODO", "Rebase TODO", "Rebase TODO", "Rebase TODO", "TODO de rebasage"},
	{"Return to commit history", "返回提交历史", "返回提交歷史", "コミット履歴に戻る", "Volver al historial de commits", "Revenir à l’historique des commits"},
	{"Compare-and-swap edit Git's complete remaining todo", "以 CAS 方式编辑 Git 完整的剩余 todo", "以 CAS 方式編輯 Git 完整的剩餘 todo", "Git の残りの TODO 全体を CAS で編集", "Editar todo el TODO restante de Git con comparación e intercambio", "Modifier tout le TODO Git restant avec comparaison-échange"},
	{"Edit TODO", "编辑 TODO", "編輯 TODO", "TODO を編集", "Editar TODO", "Modifier le TODO"},
	{"Generate a real Git rebase plan", "生成真实的 Git rebase 计划", "產生真實的 Git rebase 計畫", "実際の Git リベース計画を生成", "Generar un plan de rebase real de Git", "Générer un véritable plan de rebasage Git"},
	{"New plan", "新建计划", "新建計畫", "新しい計画", "Nuevo plan", "Nouveau plan"},
	{"Current / pending / done steps come from Git's done and git-rebase-todo files.", "当前、待处理和已完成步骤直接来自 Git 的 done 与 git-rebase-todo 文件。", "目前、待處理與已完成步驟直接來自 Git 的 done 與 git-rebase-todo 檔案。", "現在・保留・完了のステップは Git の done と git-rebase-todo ファイルから取得しています。", "Los pasos actuales, pendientes y terminados proceden de los archivos done y git-rebase-todo de Git.", "Les étapes en cours, en attente et terminées proviennent des fichiers done et git-rebase-todo de Git."},
	{"onto ", "目标 ", "目標 ", "onto ", "sobre ", "sur "},
	{"No rebase todo to display", "没有可显示的 rebase todo", "沒有可顯示的 rebase todo", "表示するリベース TODO はありません", "No hay ningún TODO de rebase para mostrar", "Aucun TODO de rebasage à afficher"},
	{"Choose a target ref to generate Git's actual plan, or start a rebase from a branch.", "选择目标引用以生成 Git 的真实计划，或从分支开始 rebase。", "選擇目標參照以產生 Git 的真實計畫，或從分支開始 rebase。", "対象の ref を選択して Git の実際の計画を生成するか、ブランチからリベースを開始してください。", "Elige una referencia de destino para generar el plan real de Git o inicia un rebase desde una rama.", "Choisissez une référence cible pour générer le véritable plan Git, ou démarrez un rebasage depuis une branche."},
	{"Rebase target ref", "Rebase 目标引用", "Rebase 目標參照", "リベース対象 ref", "Referencia de destino del rebase", "Référence cible du rebasage"},
	{"target-branch", "target-branch", "target-branch", "target-branch", "target-branch", "target-branch"},
	{"Prepare plan", "准备计划", "準備計畫", "計画を準備", "Preparar plan", "Préparer le plan"},
	{"Draft generated by Git", "由 Git 生成的草稿", "由 Git 產生的草稿", "Git が生成した下書き", "Borrador generado por Git", "Brouillon généré par Git"},
	{"Edit & start", "编辑并开始", "編輯並開始", "編集して開始", "Editar e iniciar", "Modifier et démarrer"},
	{"Marked base", "标记的基点", "標記的基點", "マークしたベース", "Base marcada", "Base marquée"},
	{"Clear mark", "清除标记", "清除標記", "マークを解除", "Quitar marca", "Effacer la marque"},
	{"The draft will use git rebase --onto <target> <full-base-SHA>.", "草稿将使用 git rebase --onto <target> <full-base-SHA>。", "草稿將使用 git rebase --onto <target> <full-base-SHA>。", "下書きは git rebase --onto <target> <full-base-SHA> を使用します。", "El borrador usará git rebase --onto <target> <full-base-SHA>.", "Le brouillon utilisera git rebase --onto <target> <full-base-SHA>."},
	{"No marked base: an ordinary rebase will not pass --onto.", "没有标记基点：普通 rebase 不会传入 --onto。", "沒有標記基點：普通 rebase 不會傳入 --onto。", "マークしたベースなし：通常のリベースでは --onto を渡しません。", "Sin base marcada: un rebase normal no pasará --onto.", "Aucune base marquée : un rebasage ordinaire ne passera pas --onto."},
	{"Updated the active rebase todo", "已更新活动的 rebase todo", "已更新活動的 rebase todo", "アクティブなリベース TODO を更新しました", "TODO del rebase activo actualizado", "TODO du rebasage actif mis à jour"},
	{"Started the rebase plan", "已开始 rebase 计划", "已開始 rebase 計畫", "リベース計画を開始しました", "Plan de rebase iniciado", "Plan de rebasage démarré"},
	{"Changed %s to %s", "已将 %s 改为 %s", "已將 %s 改為 %s", "%s を %s に変更しました", "%s cambiado a %s", "%s remplacé par %s"},
	{"Change %s to drop?", "要将 %s 改为 drop 吗？", "要將 %s 改為 drop 嗎？", "%s を drop に変更しますか？", "¿Cambiar %s a drop?", "Remplacer %s par drop ?"},
	{"The line remains in Git's todo as an explicit drop; it is not deleted.", "该行会作为明确的 drop 保留在 Git todo 中，而不是被删除。", "該行會作為明確的 drop 保留在 Git todo 中，而不是被刪除。", "この行は明示的な drop として Git の TODO に残り、削除されません。", "La línea permanece en el TODO de Git como un drop explícito; no se elimina.", "La ligne reste dans le TODO Git comme drop explicite ; elle n’est pas supprimée."},
	{"Selected pending commit:", "选中的待处理提交：", "選取的待處理提交：", "選択した保留中のコミット:", "Commit pendiente seleccionado:", "Commit en attente sélectionné :"},
	{"Rebase Plan", "Rebase 计划", "Rebase 計畫", "リベース計画", "Plan de rebase", "Plan de rebasage"},
	{"The complete plan is compared before anything is replaced.", "替换前会比较完整计划。", "替換前會比較完整計畫。", "置き換える前に計画全体を比較します。", "El plan completo se compara antes de sustituir nada.", "Le plan complet est comparé avant tout remplacement."},
	{"Close rebase plan", "关闭 rebase 计划", "關閉 rebase 計畫", "リベース計画を閉じる", "Cerrar el plan de rebase", "Fermer le plan de rebasage"},
	{"Loading Git's rebase todo", "正在加载 Git 的 rebase todo", "正在載入 Git 的 rebase todo", "Git のリベース TODO を読み込み中", "Cargando el TODO de rebase de Git", "Chargement du TODO de rebasage Git"},
	{"Loading Git's rebase todo…", "正在加载 Git 的 rebase todo", "正在載入 Git 的 rebase todo", "Git のリベース TODO を読み込み中", "Cargando el TODO de rebase de Git", "Chargement du TODO de rebasage Git"},
	{"Active todo · whole-file CAS", "活动 todo · 整文件 CAS", "活動 todo · 整檔 CAS", "アクティブ TODO · ファイル全体 CAS", "TODO activo · CAS de archivo completo", "TODO actif · CAS du fichier entier"},
	{"Draft · captured from Git", "草稿 · 从 Git 捕获", "草稿 · 從 Git 擷取", "下書き · Git から取得", "Borrador · capturado de Git", "Brouillon · capturé depuis Git"},
	{"Complete Git rebase todo", "完整的 Git rebase todo", "完整的 Git rebase todo", "Git リベース TODO 全体", "TODO completo del rebase de Git", "TODO complet du rebasage Git"},
	{"Change pick/reword/edit/squash/fixup/drop or reorder lines. Do not delete or duplicate lines; exec, update-ref, reset, label and merge directives are preserved.", "可更改 pick/reword/edit/squash/fixup/drop 或重新排序，但不能删除或复制行；exec、update-ref、reset、label 和 merge 指令会保留。", "可更改 pick/reword/edit/squash/fixup/drop 或重新排序，但不能刪除或複製行；exec、update-ref、reset、label 和 merge 指令會保留。", "pick/reword/edit/squash/fixup/drop の変更や行の並べ替えはできます。行を削除・複製しないでください。exec、update-ref、reset、label、merge ディレクティブは保持されます。", "Cambia pick/reword/edit/squash/fixup/drop o reordena las líneas. No las elimines ni las dupliques; se conservan las directivas exec, update-ref, reset, label y merge.", "Modifiez pick/reword/edit/squash/fixup/drop ou réordonnez les lignes. Ne les supprimez ni ne les dupliquez ; les directives exec, update-ref, reset, label et merge sont préservées."},
	{"Start rebase", "开始 rebase", "開始 rebase", "リベースを開始", "Iniciar rebase", "Démarrer le rebasage"},
	{"Save TODO", "保存 TODO", "儲存 TODO", "TODO を保存", "Guardar TODO", "Enregistrer le TODO"},
	{"current", "当前", "目前", "現在", "actual", "en cours"},
	{"pending", "待处理", "待處理", "保留中", "pendiente", "en attente"},
	{"done", "已完成", "已完成", "完了", "terminado", "terminé"},
}

func init() {
	for _, entry := range rebaseWorkspaceTranslations {
		Translations["zh-Hans"][entry.English] = entry.ZhHans
		Translations["zh-Hant"][entry.English] = entry.ZhHant
		Translations["ja"][entry.English] = entry.Japanese
		Translations["es"][entry.English] = entry.Spanish
		Translations["fr"][entry.English] = entry.French
	}
}
