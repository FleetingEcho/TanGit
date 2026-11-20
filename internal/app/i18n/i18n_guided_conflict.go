package i18n

// Guided conflict copy includes both direct UI strings and the stage-specific
// entries selected dynamically by guidedConflictStageExplanation.
var GuidedConflictTranslations = []struct {
	English, ZhHans, ZhHant, Japanese, Spanish, French string
}{
	{"Guided", "引导", "引導", "ガイド", "Guiado", "Guidé"},
	{"Choose icons, icons with labels, or guided conflict resolution", "选择图标、图标加标签或引导式冲突解决", "選擇圖示、圖示加標籤或引導式衝突解決", "アイコン、アイコン＋ラベル、ガイド付き競合解決を選択", "Elige iconos, iconos con etiquetas o resolución guiada de conflictos", "Choisissez des icônes, des icônes avec libellés ou la résolution guidée des conflits"},
	{"Show conflict and Changes hunk actions as icons, icons with labels, or labeled actions with step-by-step guidance", "将冲突和 Changes 差异块操作显示为图标、图标加标签，或带分步说明的引导操作", "將衝突與 Changes 差異區塊操作顯示為圖示、圖示加標籤，或帶逐步說明的引導操作", "競合と Changes ハンクの操作をアイコン、アイコンとラベル、またはステップ付きガイドで表示", "Muestra las acciones de conflicto y bloques de Changes como iconos, iconos con etiquetas o acciones guiadas con instrucciones paso a paso", "Affiche les actions de conflit et de section de Changes sous forme d’icônes, d’icônes avec libellés ou d’actions guidées avec des instructions pas à pas"},

	{"Conflict guidance", "冲突指导", "衝突指導", "競合ガイド", "Guía de conflicto", "Aide sur les conflits"},
	{"Base: common ancestor", "Base：共同祖先", "Base：共同祖先", "ベース: 共通祖先", "Base: ancestro común", "Base : ancêtre commun"},
	{"Read-only original code from Git stage 1.", "Git 阶段 1 中只读的原始代码。", "Git 階段 1 中唯讀的原始程式碼。", "Git ステージ 1 の読み取り専用の元コード。", "Código original de solo lectura de la etapa 1 de Git.", "Code d’origine en lecture seule de l’étape 1 de Git."},
	{"Compare the common ancestor with both sides before choosing a resolution.", "选择解决方案前，将共同祖先与双方进行比较。", "選擇解決方案前，將共同祖先與雙方進行比較。", "解決方法を選ぶ前に、共通祖先を両側と比較します。", "Compara el ancestro común con ambos lados antes de elegir una resolución.", "Comparez l’ancêtre commun aux deux versions avant de choisir une résolution."},
	{"Current: your branch", "Current：当前分支", "Current：目前分支", "現在: 現在のブランチ", "Actual: tu rama", "Actuel : votre branche"},
	{"Read-only current-branch content from Git stage 2.", "Git 阶段 2 中只读的当前分支内容。", "Git 階段 2 中唯讀的目前分支內容。", "Git ステージ 2 の読み取り専用の現在ブランチの内容。", "Contenido de solo lectura de tu rama, de la etapa 2 de Git.", "Contenu de votre branche en lecture seule, issu de l’étape 2 de Git."},
	{"Read what your branch changed relative to the common ancestor.", "查看当前分支相对于共同祖先的更改。", "查看目前分支相對於共同祖先的更改。", "現在のブランチが共通祖先から変更した内容を読みます。", "Lee los cambios de tu rama respecto al ancestro común.", "Lisez les modifications de votre branche par rapport à l’ancêtre commun."},
	{"Incoming: other branch", "Incoming：传入分支", "Incoming：傳入分支", "マージ側: 相手のブランチ", "Entrante: otra rama", "Entrant : autre branche"},
	{"Read-only incoming content from Git stage 3.", "Git 阶段 3 中只读的传入内容。", "Git 階段 3 中唯讀的傳入內容。", "Git ステージ 3 の読み取り専用のマージ側の内容。", "Contenido entrante de solo lectura de la etapa 3 de Git.", "Contenu entrant en lecture seule de l’étape 3 de Git."},
	{"Read what the incoming branch changed relative to the common ancestor.", "查看传入分支相对于共同祖先的更改。", "查看傳入分支相對於共同祖先的更改。", "マージ側のブランチが共通祖先から変更した内容を読みます。", "Lee los cambios de la rama entrante respecto al ancestro común.", "Lisez les modifications de la branche entrante par rapport à l’ancêtre commun."},
	{"Result: editable resolution", "Result：可编辑的解决结果", "Result：可編輯的解決結果", "結果: 編集可能な解決内容", "Resultado: resolución editable", "Résultat : résolution modifiable"},
	{"Choose a side or edit the working-tree resolution.", "选择一方或编辑工作树中的解决结果。", "選擇一方或編輯工作樹中的解決結果。", "いずれかの側を選ぶか、ワークツリーで解決内容を編集します。", "Elige un lado o edita la resolución en el árbol de trabajo.", "Choisissez une version ou modifiez la résolution dans l’arbre de travail."},
	{"Review the Result, then use Mark resolved to stage the file exactly as it is.", "检查 Result，然后使用“标记为已解决”按原样暂存文件。", "檢查 Result，然後使用「標記為已解決」按原樣暫存檔案。", "Result を確認し、「解決済みにする」でファイルをそのままステージします。", "Revisa Resultado y usa Marcar como resuelto para preparar el archivo tal como está.", "Vérifiez le Résultat, puis utilisez Marquer comme résolu pour indexer le fichier tel quel."},
	{"This stage is read-only; Result remains editable in its own view.", "此阶段为只读；Result 视图仍可编辑。", "此階段為唯讀；Result 檢視仍可編輯。", "このステージは読み取り専用です。Result ビューでは編集できます。", "Esta etapa es de solo lectura; Resultado sigue siendo editable en su propia vista.", "Cette étape est en lecture seule ; Résultat reste modifiable dans sa propre vue."},

	{"Earlier", "之前", "之前", "前の段階", "Anterior", "Précédent"},
	{"Available", "可用", "可用", "利用可能", "Disponible", "Disponible"},
	{"Next", "后续", "後續", "次の段階", "Siguiente", "Suivant"},
	{"Show guide", "显示引导", "顯示引導", "ガイドを表示", "Mostrar guía", "Afficher le guide"},
	{"Show guided conflict instructions", "显示引导式冲突说明", "顯示引導式衝突說明", "競合解決ガイドを表示", "Mostrar instrucciones guiadas de conflicto", "Afficher les instructions guidées de conflit"},
	{"Hide guide", "隐藏引导", "隱藏引導", "ガイドを隠す", "Ocultar guía", "Masquer le guide"},
	{"Hide guided conflict instructions", "隐藏引导式冲突说明", "隱藏引導式衝突說明", "競合解決ガイドを隠す", "Ocultar instrucciones guiadas de conflicto", "Masquer les instructions guidées de conflit"},
	{"Guided conflict resolution", "引导式冲突解决", "引導式衝突解決", "ガイド付き競合解決", "Resolución guiada de conflictos", "Résolution guidée des conflits"},
	{"No conflicts left to resolve", "没有待解决的冲突", "沒有待解決的衝突", "未解決の競合はありません", "No quedan conflictos por resolver", "Aucun conflit à résoudre"},
	{"File: %s", "文件：%s", "檔案：%s", "ファイル: %s", "Archivo: %s", "Fichier : %s"},
	{"Hunk %d of %d", "第 %d 个冲突块，共 %d 个", "第 %d 個衝突區塊，共 %d 個", "ハンク %d/%d", "Bloque %d de %d", "Section %d sur %d"},
	{"Read conflict", "阅读冲突", "閱讀衝突", "競合を読む", "Leer el conflicto", "Lire le conflit"},
	{"Choose or edit Result", "选择或编辑 Result", "選擇或編輯 Result", "Result を選択または編集", "Elegir o editar Resultado", "Choisir ou modifier le Résultat"},
	{"Review staged", "检查已暂存内容", "檢查已暫存內容", "ステージ済みを確認", "Revisar lo preparado", "Vérifier les changements indexés"},
	{"Conflict resolution checklist", "冲突解决清单", "衝突解決清單", "競合解決チェックリスト", "Lista de resolución de conflictos", "Liste de contrôle de la résolution des conflits"},
	{"Made a mistake here? Restore unresolved returns the saved unresolved file and Git stages.", "在这里误操作了？“恢复未解决冲突”会还原已保存的未解决文件和 Git 阶段。", "在這裡誤操作了？「還原未解決衝突」會還原已儲存的未解決檔案和 Git 階段。", "ここで誤操作しましたか？「未解決の競合を復元」は保存された未解決ファイルと Git ステージを復元します。", "¿Cometiste un error aquí? Restaurar conflicto sin resolver recupera el archivo sin resolver guardado y las etapas de Git.", "Une erreur ici ? Restaurer le conflit non résolu récupère le fichier non résolu enregistré et les étapes Git."},
	{"No recovery is available for the selected file.", "所选文件没有可用的恢复记录。", "所選檔案沒有可用的復原記錄。", "選択したファイルに利用できる復元記録はありません。", "No hay ninguna recuperación disponible para el archivo seleccionado.", "Aucune récupération n’est disponible pour le fichier sélectionné."},
	{"Undo Conflict Resolution", "撤销冲突解决", "撤銷衝突解決", "競合解決を元に戻す", "Deshacer resolución de conflictos", "Annuler la résolution du conflit"},
	{"Undo the last resolution in %s?", "要撤销 %s 中的上一次解决操作吗？", "要撤銷 %s 中的上一次解決操作嗎？", "%s の直前の解決を元に戻しますか？", "¿Deshacer la última resolución en %s?", "Annuler la dernière résolution dans %s ?"},
	{"This replaces the current Result with the text before the last resolution. The index and Git operation are not changed.", "这会将当前 Result 替换为上一次解决前的文本，不会更改 index 或 Git 操作。", "這會將目前 Result 替換為上一次解決前的文字，不會變更 index 或 Git 操作。", "現在の Result を直前の解決前のテキストに置き換えます。index と Git 操作は変更されません。", "Esto reemplaza el Result actual con el texto anterior a la última resolución. El índice y la operación de Git no cambian.", "Ceci remplace le Result actuel par le texte d’avant la dernière résolution. L’index et l’opération Git ne sont pas modifiés."},
	{"Undo resolution", "撤销解决", "撤銷解決", "解決を元に戻す", "Deshacer resolución", "Annuler la résolution"},
}

func init() {
	for _, entry := range GuidedConflictTranslations {
		Translations["zh-Hans"][entry.English] = entry.ZhHans
		Translations["zh-Hant"][entry.English] = entry.ZhHant
		Translations["ja"][entry.English] = entry.Japanese
		Translations["es"][entry.English] = entry.Spanish
		Translations["fr"][entry.English] = entry.French
	}
}
