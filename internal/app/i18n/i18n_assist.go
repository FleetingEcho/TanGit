package i18n

// Conflict Assistant keeps its source, lifecycle and safety
// vocabulary here. Keep dynamic status/source labels here as well as the statically
// audited controls so a language switch never leaves a mixed-language modal.
var assistTranslations = []struct {
	English, ZhHans, ZhHant, Japanese, Spanish, French string
}{
	{"Conflict Assistant", "冲突助手", "衝突助手", "競合アシスタント", "Asistente de conflictos", "Assistant de conflit"},
	{"AssistCommand (optional)", "AssistCommand（可选）", "AssistCommand（可選）", "AssistCommand（任意）", "AssistCommand (opcional)", "AssistCommand (facultatif)"},
	{"advisor {requestFile} {responseFile} {artifactDir}", "advisor {requestFile} {responseFile} {artifactDir}", "advisor {requestFile} {responseFile} {artifactDir}", "advisor {requestFile} {responseFile} {artifactDir}", "advisor {requestFile} {responseFile} {artifactDir}", "advisor {requestFile} {responseFile} {artifactDir}"},
	{"Conflict Assistant runs this JSON-file helper only after an explicit request. It receives only isolated request, response, and artifact paths; it never returns a Git patch.", "冲突助手仅在显式请求后运行此 JSON 文件 helper。它只接收隔离的请求、响应和 artifact 路径，绝不会返回 Git patch。", "衝突助手僅在明確請求後執行此 JSON 檔案 helper。它只接收隔離的請求、回應與 artifact 路徑，絕不會回傳 Git patch。", "競合アシスタントは、明示的に要求された場合にのみこの JSON ファイル helper を実行します。受け取るのは分離されたリクエスト、レスポンス、artifact のパスだけで、Git patch を返すことはありません。", "El asistente de conflictos ejecuta este helper de archivos JSON solo tras una solicitud explícita. Solo recibe rutas aisladas de solicitud, respuesta y artifact; nunca devuelve un parche de Git.", "L’assistant de conflit n’exécute cet helper de fichiers JSON qu’après une demande explicite. Il ne reçoit que des chemins isolés de requête, de réponse et d’artifact ; il ne renvoie jamais de patch Git."},
	{"Cancel and discard this suggestion preview", "取消并丢弃此建议预览", "取消並捨棄此建議預覽", "この提案プレビューをキャンセルして破棄", "Cancelar y descartar esta vista previa de sugerencias", "Annuler et abandonner cet aperçu des suggestions"},
	{"Suggestion preview", "建议预览", "建議預覽", "提案プレビュー", "Vista previa de sugerencias", "Aperçu des suggestions"},
	{"Suggestion source:", "建议来源：", "建議來源：", "提案ソース：", "Origen de la sugerencia:", "Source de la suggestion :"},
	{"Preview region substitutions on a clone. Accept writes Result only and saves V2 recovery.", "在克隆上预览各区域替换。接受只写入 Result，并保存 V2 recovery。", "在複本上預覽各區域替換。接受僅寫入 Result，並儲存 V2 recovery。", "クローン上で領域の置換をプレビューします。Accept は Result のみを書き込み、V2 recovery を保存します。", "Previsualiza sustituciones por región en un clon. Accept escribe solo Result y guarda la recuperación V2.", "Prévisualise les substitutions par région sur un clone. Accept écrit uniquement Result et enregistre la récupération V2."},
	{"Conflicted path:", "冲突路径：", "衝突路徑：", "競合パス：", "Ruta en conflicto:", "Chemin en conflit :"},
	{"Requesting suggestions…", "正在请求建议…", "正在請求建議…", "提案を要求しています…", "Solicitando sugerencias…", "Demande de suggestions…"},
	{"The source Result and Git state are read-only while the helper runs.", "helper 运行期间，源 Result 和 Git 状态保持只读。", "helper 執行期間，來源 Result 與 Git 狀態保持唯讀。", "ヘルパーの実行中、ソース Result と Git の状態は読み取り専用です。", "El Result de origen y el estado de Git son de solo lectura mientras se ejecuta el helper.", "Le Result source et l’état Git restent en lecture seule pendant l’exécution de l’helper."},
	{"Suggestions unavailable", "建议不可用", "建議不可用", "提案を利用できません", "Sugerencias no disponibles", "Suggestions indisponibles"},
	{"No suggestions yet", "还没有建议", "尚無建議", "提案はまだありません", "Aún no hay sugerencias", "Aucune suggestion pour le moment"},
	{"Choose Get region suggestions explicitly. Opening this dialog never runs external AI or changes your files.", "请显式选择“获取区域建议”。打开此对话框绝不会运行外部 AI 或修改文件。", "請明確選擇「取得區域建議」。開啟此對話框絕不會執行外部 AI 或修改檔案。", "「領域の提案を取得」を明示的に選択してください。このダイアログを開くだけでは外部 AI を実行せず、ファイルも変更しません。", "Elige explícitamente Obtener sugerencias de regiones. Abrir este diálogo nunca ejecuta IA externa ni modifica tus archivos.", "Choisissez explicitement Obtenir les suggestions par région. L’ouverture de cette boîte n’exécute jamais d’IA externe et ne modifie pas vos fichiers."},
	{"Region suggestions and reasons", "区域建议及理由", "區域建議及理由", "領域の提案と理由", "Sugerencias y motivos por región", "Suggestions et raisons par région"},
	{"Region %d · original lines %d–%d", "区域 %d · 原行 %d–%d", "區域 %d · 原行 %d–%d", "領域 %d · 元の行 %d–%d", "Región %d · líneas originales %d–%d", "Région %d · lignes d’origine %d–%d"},
	{"Excluded; Accept is disabled for this incomplete Result", "已排除；Result 不完整，因此接受已禁用", "已排除；Result 不完整，因此接受已停用", "除外済み。Result が不完全なため Accept は無効です", "Excluido; Accept está desactivado para este Result incompleto", "Exclu ; Accept est désactivé pour ce Result incomplet"},
	{"Reason:", "理由：", "理由：", "理由：", "Motivo:", "Raison :"},
	{"No suggestion reason was supplied.", "未提供建议理由。", "未提供建議理由。", "提案の理由は提供されませんでした。", "No se proporcionó un motivo para la sugerencia.", "Aucune raison n’a été fournie pour la suggestion."},
	{"Substitution:", "替换方式：", "替換方式：", "置換：", "Sustitución:", "Substitution :"},
	{"Result before/after preview", "Result 前后预览", "Result 前後預覽", "Result の前後プレビュー", "Vista previa de Result antes/después", "Aperçu avant/après de Result"},
	{"Computed in memory from a cloned conflict state. No working-tree write, undo edit, stage, continue, commit, or hook has run.", "根据克隆的 conflictState 在内存中计算。未写入 working tree、修改 undo、stage、continue、commit 或运行 hook。", "根據複製的 conflictState 在記憶體中計算。未寫入 working tree、修改 undo、stage、continue、commit 或執行 hook。", "クローンした conflictState からメモリ内で計算します。working tree への書き込み、undo の変更、stage、continue、commit、hook は実行されていません。", "Calculado en memoria desde un conflictState clonado. No se ha escrito en el árbol de trabajo, editado undo, preparado, continuado, confirmado ni ejecutado hooks.", "Calculé en mémoire depuis un conflictState cloné. Aucune écriture dans le working tree, modification d’undo, indexation, continuation, commit ou hook n’a été exécuté."},
	{"Accept requires a suggestion for every original region; explicit exclusions are preserved only for review.", "接受要求每个原始区域都有建议；显式排除仅供审阅，不会被接受。", "接受要求每個原始區域都有建議；明確排除僅供審閱，不會被接受。", "Accept には元のすべての領域への提案が必要です。明示的な除外は確認用にのみ保持されます。", "Accept requiere una sugerencia para cada región original; las exclusiones explícitas se conservan solo para revisión.", "Accept exige une suggestion pour chaque région d’origine ; les exclusions explicites sont conservées uniquement pour revue."},
	{"Cancel request", "取消请求", "取消請求", "要求をキャンセル", "Cancelar solicitud", "Annuler la demande"},
	{"Retire this generation; a late response cannot be accepted", "使此 generation 过期；迟到响应将无法被接受", "使此 generation 過期；延遲回應將無法被接受", "この generation を無効化します。遅延応答は受理できません", "Invalida esta generación; una respuesta tardía no podrá aceptarse", "Invalide cette génération ; une réponse tardive ne pourra pas être acceptée"},
	{"Request region suggestions from the configured JSON-file helper", "从已配置的 JSON 文件 helper 请求区域建议", "從已設定的 JSON 檔案 helper 請求區域建議", "設定済み JSON ファイルヘルパーに領域の提案を要求", "Solicitar sugerencias por región al helper JSON configurado", "Demander des suggestions par région à l’helper JSON configuré"},
	{"Add an AssistCommand above; no external helper runs without one", "请在上方添加 AssistCommand；未配置时不会运行外部 helper", "請在上方新增 AssistCommand；未設定時不會執行外部 helper", "上の AssistCommand を追加してください。未設定では外部ヘルパーを実行しません", "Añade un AssistCommand arriba; sin él no se ejecuta ningún helper externo", "Ajoutez un AssistCommand ci-dessus ; sans lui aucun helper externe ne s’exécute"},
	{"Get region suggestions", "获取区域建议", "取得區域建議", "領域の提案を取得", "Obtener sugerencias por región", "Obtenir des suggestions par région"},
	{"Validate path, raw bytes, index stages, and operation fingerprint; then write Result only", "核对 path、原始字节、index stages 和 operation fingerprint，然后只写入 Result", "核對 path、原始位元組、index stages 與 operation fingerprint，然後僅寫入 Result", "path、raw bytes、index stages、operation fingerprint を検証してから Result のみを書き込みます", "Valida la ruta, bytes brutos, etapas del índice y fingerprint de operación; después escribe solo Result", "Valide le chemin, les octets bruts, les étapes d’index et l’empreinte d’opération, puis écrit uniquement Result"},
	{"Accept is unavailable while any region is explicitly excluded", "存在显式排除区域时无法接受", "存在明確排除區域時無法接受", "明示的に除外された領域がある間は Accept できません", "Accept no está disponible mientras alguna región esté excluida explícitamente", "Accept est indisponible tant qu’une région est explicitement exclue"},
	{"Accept Result only", "仅接受 Result", "僅接受 Result", "Result のみを受理", "Aceptar solo Result", "Accepter Result uniquement"},
	{"Preview region suggestions on a clone; Accept writes Result only", "在克隆上预览区域建议；接受只写入 Result", "在複本上預覽區域建議；接受僅寫入 Result", "クローンで領域の提案をプレビュー。Accept は Result のみを書き込みます", "Previsualiza sugerencias por región en un clon; Accept escribe solo Result", "Prévisualise les suggestions par région sur un clone ; Accept écrit uniquement Result"},

	// Dynamic source and lifecycle labels.
	{"Not configured", "未配置", "未設定", "未設定", "No configurado", "Non configuré"},
	{"External Agent Profile helper", "外部 Agent Profile helper", "外部 Agent Profile helper", "外部 Agent Profile ヘルパー", "Helper externo de Agent Profile", "Helper externe de Agent Profile"},
	{"Injected preview advisor", "注入的建议预览器", "注入的建議預覽器", "注入されたプレビューアドバイザー", "Asesor de vista previa inyectado", "Conseiller d’aperçu injecté"},
	{"No suggestions requested", "尚未请求建议", "尚未請求建議", "提案は未要求です", "No se han solicitado sugerencias", "Aucune suggestion demandée"},
	{"Source unavailable", "源不可用", "來源不可用", "ソースを利用できません", "Origen no disponible", "Source indisponible"},
	{"Requesting validated region suggestions", "正在请求经过校验的区域建议", "正在請求經過驗證的區域建議", "検証済み領域の提案を要求しています", "Solicitando sugerencias por región validadas", "Demande de suggestions par région validées"},
	{"Advice rejected", "建议已拒绝", "建議已拒絕", "提案を拒否しました", "Sugerencia rechazada", "Suggestion rejetée"},
	{"Source changed", "源已变化", "來源已變更", "ソースが変更されました", "El origen cambió", "La source a changé"},
	{"Validated suggestions ready; no file or Git state changed", "建议已通过校验；未修改文件或 Git 状态", "建議已通過驗證；未修改檔案或 Git 狀態", "検証済み提案の準備完了。ファイルと Git 状態は未変更です", "Sugerencias validadas; no se modificó ningún archivo ni el estado de Git", "Suggestions validées ; aucun fichier ni état Git n’a été modifié"},
	{"Accept refused", "已拒绝接受", "已拒絕接受", "受理を拒否しました", "Aceptación rechazada", "Acceptation refusée"},
	{"Accept unavailable", "无法接受", "無法接受", "受理できません", "Aceptación no disponible", "Acceptation indisponible"},
	{"Request canceled; nothing was written", "请求已取消；未写入任何内容", "請求已取消；未寫入任何內容", "要求をキャンセルしました。何も書き込まれていません", "Solicitud cancelada; no se escribió nada", "Demande annulée ; rien n’a été écrit"},

	// Result-only and index-only completion messages.
	{"Accepted Conflict Assistant Result", "已接受 Conflict Assistant 的 Result", "已接受 Conflict Assistant 的 Result", "Conflict Assistant の Result を受理しました", "Result de Conflict Assistant aceptado", "Result de Conflict Assistant accepté"},
	{"Conflict Result saved", "冲突 Result 已保存", "衝突 Result 已儲存", "競合 Result を保存しました", "Result de conflicto guardado", "Result de conflit enregistré"},
	{"Only Result changed; index and operation were preserved. V2 recovery: %s", "仅 Result 已更改；index 和 operation 保持不变。V2 recovery：%s", "僅 Result 已變更；index 與 operation 保持不變。V2 recovery：%s", "Result のみを変更し、index と operation は保持されました。V2 recovery: %s", "Solo cambió Result; se conservaron el índice y la operación. Recuperación V2: %s", "Seul Result a changé ; l’index et l’opération ont été préservés. Récupération V2 : %s"},
}

func init() {
	for _, entry := range assistTranslations {
		Translations["zh-Hans"][entry.English] = entry.ZhHans
		Translations["zh-Hant"][entry.English] = entry.ZhHant
		Translations["ja"][entry.English] = entry.Japanese
		Translations["es"][entry.English] = entry.Spanish
		Translations["fr"][entry.English] = entry.French
	}
}
