package i18n

var cockpitAttentionTranslations = map[string]map[string]string{
	"zh-Hans": {
		"Continue the interrupted %s":                             "继续中断的 %s 操作",
		"Resolve %d conflicted files":                             "解决 %d 个冲突文件",
		"Save %d unsaved editor buffers":                          "保存 %d 个未保存的编辑器缓冲区",
		"%d Agent tasks need input":                               "%d 个 Agent 任务需要输入",
		"%d Agent tasks have failed tests":                        "%d 个 Agent 任务的测试失败",
		"%d Agent tasks are blocked":                              "%d 个 Agent 任务被阻塞",
		"%d Agent results are ready for review":                   "%d 个 Agent 成果可以审阅",
		"%d transfers failed or completed partially":              "%d 个传输失败或部分完成",
		"Open Remotes to inspect the failed or partial transfer.": "打开远程仓库，检查失败或部分完成的传输。",
		"Push %d local commits":                                   "推送 %d 个本地提交",
		"Pull %d upstream commits":                                "拉取 %d 个上游提交",
		"Commit %d staged files":                                  "提交 %d 个已暂存文件",
	},
	"zh-Hant": {
		"Continue the interrupted %s":                             "繼續中斷的 %s 操作",
		"Resolve %d conflicted files":                             "解決 %d 個衝突檔案",
		"Save %d unsaved editor buffers":                          "儲存 %d 個未儲存的編輯器緩衝區",
		"%d Agent tasks need input":                               "%d 個 Agent 任務需要輸入",
		"%d Agent tasks have failed tests":                        "%d 個 Agent 任務的測試失敗",
		"%d Agent tasks are blocked":                              "%d 個 Agent 任務被阻塞",
		"%d Agent results are ready for review":                   "%d 個 Agent 成果可以審閱",
		"%d transfers failed or completed partially":              "%d 個傳輸失敗或部分完成",
		"Open Remotes to inspect the failed or partial transfer.": "開啟遠端倉庫，檢查失敗或部分完成的傳輸。",
		"Push %d local commits":                                   "推送 %d 個本地提交",
		"Pull %d upstream commits":                                "拉取 %d 個上游提交",
		"Commit %d staged files":                                  "提交 %d 個已暫存檔案",
	},
	"ja": {
		"Continue the interrupted %s":                             "中断した %s 操作を続行",
		"Resolve %d conflicted files":                             "%d 個の競合ファイルを解決",
		"Save %d unsaved editor buffers":                          "%d 個の未保存エディターバッファーを保存",
		"%d Agent tasks need input":                               "%d 個の Agent タスクで入力が必要です",
		"%d Agent tasks have failed tests":                        "%d 個の Agent タスクでテストが失敗しています",
		"%d Agent tasks are blocked":                              "%d 個の Agent タスクがブロックされています",
		"%d Agent results are ready for review":                   "%d 個の Agent 結果をレビューできます",
		"%d transfers failed or completed partially":              "%d 個の転送が失敗または部分的に完了しました",
		"Open Remotes to inspect the failed or partial transfer.": "リモートを開いて、失敗または部分的に完了した転送を確認してください。",
		"Push %d local commits":                                   "%d 個のローカルコミットをプッシュ",
		"Pull %d upstream commits":                                "%d 個のアップストリームコミットをプル",
		"Commit %d staged files":                                  "%d 個のステージ済みファイルをコミット",
	},
	"es": {
		"Continue the interrupted %s":                             "Continuar la operación interrumpida %s",
		"Resolve %d conflicted files":                             "Resolver %d archivos en conflicto",
		"Save %d unsaved editor buffers":                          "Guardar %d búferes del editor sin guardar",
		"%d Agent tasks need input":                               "%d tareas de Agent necesitan información",
		"%d Agent tasks have failed tests":                        "%d tareas de Agent tienen pruebas fallidas",
		"%d Agent tasks are blocked":                              "%d tareas de Agent están bloqueadas",
		"%d Agent results are ready for review":                   "%d resultados de Agent están listos para revisar",
		"%d transfers failed or completed partially":              "%d transferencias fallaron o se completaron parcialmente",
		"Open Remotes to inspect the failed or partial transfer.": "Abre Remotos para inspeccionar las transferencias fallidas o parciales.",
		"Push %d local commits":                                   "Hacer push de %d commits locales",
		"Pull %d upstream commits":                                "Hacer pull de %d commits remotos",
		"Commit %d staged files":                                  "Confirmar %d archivos preparados",
	},
	"fr": {
		"Continue the interrupted %s":                             "Poursuivre l’opération interrompue %s",
		"Resolve %d conflicted files":                             "Résoudre %d fichiers en conflit",
		"Save %d unsaved editor buffers":                          "Enregistrer %d tampons non enregistrés de l’éditeur",
		"%d Agent tasks need input":                               "%d tâches Agent nécessitent une saisie",
		"%d Agent tasks have failed tests":                        "%d tâches Agent ont des tests en échec",
		"%d Agent tasks are blocked":                              "%d tâches Agent sont bloquées",
		"%d Agent results are ready for review":                   "%d résultats Agent sont prêts à être examinés",
		"%d transfers failed or completed partially":              "%d transferts ont échoué ou sont partiellement terminés",
		"Open Remotes to inspect the failed or partial transfer.": "Ouvrez Dépôts distants pour inspecter les transferts échoués ou partiels.",
		"Push %d local commits":                                   "Publier %d commits locaux",
		"Pull %d upstream commits":                                "Récupérer %d commits amont",
		"Commit %d staged files":                                  "Valider %d fichiers indexés",
	},
}

func init() {
	for language, entries := range cockpitAttentionTranslations {
		for english, translated := range entries {
			Translations[language][english] = translated
		}
	}
}
