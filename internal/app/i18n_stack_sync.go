package app

// Stack sync copy names the exact Git operation and keeps publication
// limitations explicit in every supported language.
var stackSyncTranslations = map[string]map[string]string{
	"zh-Hans": {
		"ff": "快进", "remote rewritten": "远程历史已重写", "local ahead only": "仅本地领先", "up-to-date": "已是最新", "diverged": "已分叉", "no upstream": "无上游", "upstream missing": "上游缺失",
		"CAS update-ref → %s": "通过 CAS update-ref 更新到 %s", "Real merge --ff-only → %s": "执行 merge --ff-only 到 %s", "Real merge → %s": "执行 merge 到 %s", "Real rebase onto %s from %s": "实际变基：目标和来源为 %s、%s", "Skip local update": "跳过本地更新", "Publish with preview-OID lease": "使用预览 OID 租约发布", "No local change": "本地无需更改",
		"%s → %s": "%s → %s", "parent %s": "父分支 %s", "ahead %d · behind %d": "领先 %d · 落后 %d", "upstream %s/%s · %s": "上游 %s/%s · %s", "worktree %s%s": "工作树 %s%s", " · live %s": " · 进行中：%s",
		"rebase.updateRefs enabled: Git's real TODO owns matching refs; the CAS batch will not update them again.": "已启用 rebase.updateRefs：Git 的实际 TODO 负责对应 ref，CAS 批次不会重复更新。",
		"Base %s · %s": "基线 %s · %s", "Current %s · plan %s": "当前 %s · 计划 %s", "Excluded %d branch%s already merged into %s.": "已排除 %d 个分支%s（均已合并到 %s）。",
		"rebase.updateRefs is enabled. Refs actually listed in Git's generated TODO will be reported as rebase-driven.": "已启用 rebase.updateRefs。结果会标明由 Git 生成的实际 TODO 驱动的 ref。",
		"No unmerged local branches need review.": "没有需要审查的未合并本地分支。", "Local branch results": "本地分支结果", " · %s → %s": " · %s → %s", "skipped": "已跳过", "Driven by actual rebase TODO: %s": "由实际 rebase TODO 驱动：%s",
		"Remote publication · independent, not transactional": "远程发布 · 各自独立，非事务操作", "%d/%d published": "已发布 %d/%d", "Published": "已发布", "Failed": "失败", "%s · %s → %s/%s · %s": "%s · %s → %s/%s · %s",
		"force-with-lease expected remote %s": "force-with-lease 预期的远端 %s", "Each remote branch was attempted independently. A failure does not roll back another remote or any local update.": "每个远程分支均独立尝试。失败不会回滚其他远端或任何本地更新。",
		"Sync stack": "同步堆栈", "Review the frozen Git operation for every branch.": "审查每个分支已冻结的 Git 操作。", "Close stack sync": "关闭堆栈同步", "Publish changed branches": "发布已更改的分支", "Divergence strategy": "分叉处理策略",
		"Every publish uses force-with-lease bound to that preview's remote OID. Pushes are independent, not a remote transaction.": "每次发布都使用绑定预览远端 OID 的 force-with-lease。推送相互独立，不构成远端事务。",
		"Building a fresh stack sync plan": "正在生成新的堆栈同步计划", "Reading live refs, topology, upstreams, worktrees, and operations…": "正在读取实时 ref、拓扑、上游、工作树和操作…",
		"Stack sync results": "堆栈同步结果", "Stack sync finished with %d publication failure%s": "堆栈同步完成，发布失败 %d 项%s", "Provide a fresh plan to review stack synchronization.": "请生成新计划以审查堆栈同步。", "Refresh plan": "刷新计划", "New plan": "新建计划", "Apply plan": "应用计划",
		"Sync stack…": "同步堆栈…", "Preview frozen per-branch updates and preview-OID-leased publishes": "预览已冻结的逐分支更新及绑定预览 OID 租约的发布",
	},
	"zh-Hant": {
		"ff": "快轉", "remote rewritten": "遠端歷史已重寫", "local ahead only": "僅本機領先", "up-to-date": "已是最新", "diverged": "已分叉", "no upstream": "無上游", "upstream missing": "上游缺失",
		"CAS update-ref → %s": "透過 CAS update-ref 更新到 %s", "Real merge --ff-only → %s": "執行 merge --ff-only 到 %s", "Real merge → %s": "執行 merge 到 %s", "Real rebase onto %s from %s": "實際變基：目標和來源為 %s、%s", "Skip local update": "略過本機更新", "Publish with preview-OID lease": "使用預覽 OID 租約發布", "No local change": "本機無需變更",
		"%s → %s": "%s → %s", "parent %s": "父分支 %s", "ahead %d · behind %d": "領先 %d · 落後 %d", "upstream %s/%s · %s": "上游 %s/%s · %s", "worktree %s%s": "工作樹 %s%s", " · live %s": " · 進行中：%s",
		"rebase.updateRefs enabled: Git's real TODO owns matching refs; the CAS batch will not update them again.": "已啟用 rebase.updateRefs：Git 的實際 TODO 負責對應 ref，CAS 批次不會重複更新。",
		"Base %s · %s": "基線 %s · %s", "Current %s · plan %s": "目前 %s · 計畫 %s", "Excluded %d branch%s already merged into %s.": "已排除 %d 個分支%s（均已合併到 %s）。",
		"rebase.updateRefs is enabled. Refs actually listed in Git's generated TODO will be reported as rebase-driven.": "已啟用 rebase.updateRefs。結果會標明由 Git 產生的實際 TODO 驅動的 ref。",
		"No unmerged local branches need review.": "沒有需要審查的未合併本機分支。", "Local branch results": "本機分支結果", " · %s → %s": " · %s → %s", "skipped": "已略過", "Driven by actual rebase TODO: %s": "由實際 rebase TODO 驅動：%s",
		"Remote publication · independent, not transactional": "遠端發布 · 各自獨立，非交易操作", "%d/%d published": "已發布 %d/%d", "Published": "已發布", "Failed": "失敗", "%s · %s → %s/%s · %s": "%s · %s → %s/%s · %s",
		"force-with-lease expected remote %s": "force-with-lease 預期的遠端 %s", "Each remote branch was attempted independently. A failure does not roll back another remote or any local update.": "每個遠端分支均獨立嘗試。失敗不會復原其他遠端或任何本機更新。",
		"Sync stack": "同步堆疊", "Review the frozen Git operation for every branch.": "審查每個分支已凍結的 Git 操作。", "Close stack sync": "關閉堆疊同步", "Publish changed branches": "發布已變更的分支", "Divergence strategy": "分叉處理策略",
		"Every publish uses force-with-lease bound to that preview's remote OID. Pushes are independent, not a remote transaction.": "每次發布都使用綁定預覽遠端 OID 的 force-with-lease。推送相互獨立，不構成遠端交易。",
		"Building a fresh stack sync plan": "正在產生新的堆疊同步計畫", "Reading live refs, topology, upstreams, worktrees, and operations…": "正在讀取即時 ref、拓撲、上游、工作樹和操作…",
		"Stack sync results": "堆疊同步結果", "Stack sync finished with %d publication failure%s": "堆疊同步完成，發布失敗 %d 項%s", "Provide a fresh plan to review stack synchronization.": "請產生新計畫以審查堆疊同步。", "Refresh plan": "重新整理計畫", "New plan": "新增計畫", "Apply plan": "套用計畫",
		"Sync stack…": "同步堆疊…", "Preview frozen per-branch updates and preview-OID-leased publishes": "預覽已凍結的逐分支更新及綁定預覽 OID 租約的發布",
	},
	"ja": {
		"ff": "fast-forward", "remote rewritten": "リモート履歴が書き換え済み", "local ahead only": "ローカルのみ進んでいます", "up-to-date": "最新", "diverged": "分岐", "no upstream": "上流なし", "upstream missing": "上流が見つかりません",
		"CAS update-ref → %s": "CAS update-ref で %s へ更新", "Real merge --ff-only → %s": "merge --ff-only を %s へ実行", "Real merge → %s": "merge を %s へ実行", "Real rebase onto %s from %s": "リベース：対象と元は %s、%s", "Skip local update": "ローカル更新をスキップ", "Publish with preview-OID lease": "プレビュー OID リースで公開", "No local change": "ローカル変更なし",
		"%s → %s": "%s → %s", "parent %s": "親 %s", "ahead %d · behind %d": "先行 %d · 遅延 %d", "upstream %s/%s · %s": "上流 %s/%s · %s", "worktree %s%s": "ワークツリー %s%s", " · live %s": " · 実行中 %s",
		"rebase.updateRefs enabled: Git's real TODO owns matching refs; the CAS batch will not update them again.": "rebase.updateRefs が有効です。Git の実際の TODO が対象 ref を管理し、CAS バッチは再更新しません。",
		"Base %s · %s": "ベース %s · %s", "Current %s · plan %s": "現在 %s · プラン %s", "Excluded %d branch%s already merged into %s.": "%d 個のブランチ%sを除外しました（すべて %s にマージ済み）。",
		"rebase.updateRefs is enabled. Refs actually listed in Git's generated TODO will be reported as rebase-driven.": "rebase.updateRefs が有効です。Git が生成した実際の TODO にある ref は rebase 駆動として表示されます。",
		"No unmerged local branches need review.": "確認が必要な未マージのローカルブランチはありません。", "Local branch results": "ローカルブランチの結果", " · %s → %s": " · %s → %s", "skipped": "スキップ", "Driven by actual rebase TODO: %s": "実際の rebase TODO による更新: %s",
		"Remote publication · independent, not transactional": "リモート公開 · 独立実行・非トランザクション", "%d/%d published": "%d/%d を公開", "Published": "公開済み", "Failed": "失敗", "%s · %s → %s/%s · %s": "%s · %s → %s/%s · %s",
		"force-with-lease expected remote %s": "force-with-lease の想定リモート %s", "Each remote branch was attempted independently. A failure does not roll back another remote or any local update.": "各リモートブランチは独立して処理されます。失敗しても他のリモートやローカル更新はロールバックされません。",
		"Sync stack": "スタックを同期", "Review the frozen Git operation for every branch.": "ブランチごとの確定済み Git 操作を確認します。", "Close stack sync": "スタック同期を閉じる", "Publish changed branches": "変更ブランチを公開", "Divergence strategy": "分岐の処理方法",
		"Every publish uses force-with-lease bound to that preview's remote OID. Pushes are independent, not a remote transaction.": "各公開はプレビューのリモート OID に結び付いた force-with-lease を使います。プッシュは独立しており、リモートトランザクションではありません。",
		"Building a fresh stack sync plan": "新しいスタック同期プランを作成中", "Reading live refs, topology, upstreams, worktrees, and operations…": "最新の ref・ topology・上流・ワークツリー・操作を読み込み中…",
		"Stack sync results": "スタック同期の結果", "Stack sync finished with %d publication failure%s": "スタック同期が完了し、公開失敗は %d 件%sです", "Provide a fresh plan to review stack synchronization.": "新しいプランを生成してスタック同期を確認してください。", "Refresh plan": "プランを更新", "New plan": "新しいプラン", "Apply plan": "プランを適用",
		"Sync stack…": "スタックを同期…", "Preview frozen per-branch updates and preview-OID-leased publishes": "確定済みのブランチ別更新とプレビュー OID リース付き公開をプレビュー",
	},
	"es": {
		"ff": "avance rápido", "remote rewritten": "remoto reescrito", "local ahead only": "solo local adelantada", "up-to-date": "actualizada", "diverged": "divergente", "no upstream": "sin upstream", "upstream missing": "upstream ausente",
		"CAS update-ref → %s": "CAS update-ref hacia %s", "Real merge --ff-only → %s": "merge --ff-only real hacia %s", "Real merge → %s": "merge real hacia %s", "Real rebase onto %s from %s": "rebase real: destino y origen %s, %s", "Skip local update": "Omitir actualización local", "Publish with preview-OID lease": "Publicar con lease del OID de vista previa", "No local change": "Sin cambios locales",
		"%s → %s": "%s → %s", "parent %s": "rama padre %s", "ahead %d · behind %d": "adelantada %d · atrasada %d", "upstream %s/%s · %s": "upstream %s/%s · %s", "worktree %s%s": "worktree %s%s", " · live %s": " · %s en curso",
		"rebase.updateRefs enabled: Git's real TODO owns matching refs; the CAS batch will not update them again.": "rebase.updateRefs está activado: el TODO real de Git controla los refs correspondientes y el lote CAS no los actualizará de nuevo.",
		"Base %s · %s": "Base %s · %s", "Current %s · plan %s": "Actual %s · plan %s", "Excluded %d branch%s already merged into %s.": "Se excluyeron %d rama%s ya fusionadas en %s.",
		"rebase.updateRefs is enabled. Refs actually listed in Git's generated TODO will be reported as rebase-driven.": "rebase.updateRefs está activado. Los refs incluidos en el TODO generado por Git se mostrarán como controlados por rebase.",
		"No unmerged local branches need review.": "No hay ramas locales sin fusionar que revisar.", "Local branch results": "Resultados de ramas locales", " · %s → %s": " · %s → %s", "skipped": "omitida", "Driven by actual rebase TODO: %s": "Controlado por el TODO real de rebase: %s",
		"Remote publication · independent, not transactional": "Publicación remota · independiente, no transaccional", "%d/%d published": "%d/%d publicadas", "Published": "Publicada", "Failed": "Error", "%s · %s → %s/%s · %s": "%s · %s → %s/%s · %s",
		"force-with-lease expected remote %s": "remoto esperado por force-with-lease %s", "Each remote branch was attempted independently. A failure does not roll back another remote or any local update.": "Cada rama remota se procesó por separado. Un error no revierte otro remoto ni ninguna actualización local.",
		"Sync stack": "Sincronizar pila", "Review the frozen Git operation for every branch.": "Revisa la operación Git congelada de cada rama.", "Close stack sync": "Cerrar sincronización de pila", "Publish changed branches": "Publicar ramas modificadas", "Divergence strategy": "Estrategia de divergencia",
		"Every publish uses force-with-lease bound to that preview's remote OID. Pushes are independent, not a remote transaction.": "Cada publicación usa force-with-lease vinculado al OID remoto de la vista previa. Los pushes son independientes, no una transacción remota.",
		"Building a fresh stack sync plan": "Creando un nuevo plan de sincronización", "Reading live refs, topology, upstreams, worktrees, and operations…": "Leyendo refs, topología, upstreams, worktrees y operaciones actuales…",
		"Stack sync results": "Resultados de sincronización de pila", "Stack sync finished with %d publication failure%s": "La sincronización terminó con %d error%s de publicación", "Provide a fresh plan to review stack synchronization.": "Genera un plan nuevo para revisar la sincronización.", "Refresh plan": "Actualizar plan", "New plan": "Nuevo plan", "Apply plan": "Aplicar plan",
		"Sync stack…": "Sincronizar pila…", "Preview frozen per-branch updates and preview-OID-leased publishes": "Previsualiza actualizaciones por rama congeladas y publicaciones con lease del OID de vista previa",
	},
	"fr": {
		"ff": "avance rapide", "remote rewritten": "distant réécrit", "local ahead only": "avance locale uniquement", "up-to-date": "à jour", "diverged": "divergente", "no upstream": "sans amont", "upstream missing": "amont absent",
		"CAS update-ref → %s": "CAS update-ref vers %s", "Real merge --ff-only → %s": "merge --ff-only réel vers %s", "Real merge → %s": "merge réel vers %s", "Real rebase onto %s from %s": "rebase réel : destination et source %s, %s", "Skip local update": "Ignorer la mise à jour locale", "Publish with preview-OID lease": "Publier avec bail de l’OID prévisualisé", "No local change": "Aucun changement local",
		"%s → %s": "%s → %s", "parent %s": "parent %s", "ahead %d · behind %d": "avance %d · retard %d", "upstream %s/%s · %s": "amont %s/%s · %s", "worktree %s%s": "worktree %s%s", " · live %s": " · %s en cours",
		"rebase.updateRefs enabled: Git's real TODO owns matching refs; the CAS batch will not update them again.": "rebase.updateRefs est activé : le TODO réel de Git gère les refs correspondantes et le lot CAS ne les mettra pas de nouveau à jour.",
		"Base %s · %s": "Base %s · %s", "Current %s · plan %s": "Actuelle %s · plan %s", "Excluded %d branch%s already merged into %s.": "%d branche%s déjà fusionnées dans %s ont été exclues.",
		"rebase.updateRefs is enabled. Refs actually listed in Git's generated TODO will be reported as rebase-driven.": "rebase.updateRefs est activé. Les refs du TODO réel généré par Git seront signalées comme pilotées par rebase.",
		"No unmerged local branches need review.": "Aucune branche locale non fusionnée à examiner.", "Local branch results": "Résultats des branches locales", " · %s → %s": " · %s → %s", "skipped": "ignorée", "Driven by actual rebase TODO: %s": "Piloté par le TODO réel de rebase : %s",
		"Remote publication · independent, not transactional": "Publication distante · indépendante, non transactionnelle", "%d/%d published": "%d/%d publiées", "Published": "Publiée", "Failed": "Échec", "%s · %s → %s/%s · %s": "%s · %s → %s/%s · %s",
		"force-with-lease expected remote %s": "distant attendu par force-with-lease %s", "Each remote branch was attempted independently. A failure does not roll back another remote or any local update.": "Chaque branche distante a été traitée indépendamment. Un échec n’annule ni un autre distant ni une mise à jour locale.",
		"Sync stack": "Synchroniser la pile", "Review the frozen Git operation for every branch.": "Examinez l’opération Git figée de chaque branche.", "Close stack sync": "Fermer la synchronisation de pile", "Publish changed branches": "Publier les branches modifiées", "Divergence strategy": "Stratégie de divergence",
		"Every publish uses force-with-lease bound to that preview's remote OID. Pushes are independent, not a remote transaction.": "Chaque publication utilise force-with-lease lié à l’OID distant de l’aperçu. Les pushes sont indépendants, pas une transaction distante.",
		"Building a fresh stack sync plan": "Création d’un nouveau plan de synchronisation", "Reading live refs, topology, upstreams, worktrees, and operations…": "Lecture des refs, topologie, amonts, worktrees et opérations actuels…",
		"Stack sync results": "Résultats de synchronisation de pile", "Stack sync finished with %d publication failure%s": "Synchronisation terminée avec %d échec%s de publication", "Provide a fresh plan to review stack synchronization.": "Générez un nouveau plan pour examiner la synchronisation.", "Refresh plan": "Actualiser le plan", "New plan": "Nouveau plan", "Apply plan": "Appliquer le plan",
		"Sync stack…": "Synchroniser la pile…", "Preview frozen per-branch updates and preview-OID-leased publishes": "Prévisualisez les mises à jour figées par branche et les publications avec bail de l’OID prévisualisé",
	},
}

func init() {
	catalogs := map[string]map[string]string{"zh-Hans": zhHans, "zh-Hant": zhHant, "ja": japanese, "es": spanish, "fr": french}
	for language, additions := range stackSyncTranslations {
		for english, translated := range additions {
			catalogs[language][english] = translated
		}
	}
}
