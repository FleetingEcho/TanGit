package app

import (
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"lazygitapp/internal/agentwork"
)

// agentTaskCacheDirectory scopes a relative cache explicitly to its task
// worktree. Absolute paths remain exactly what the trusted profile requested.
func agentTaskCacheDirectory(task agentwork.Task) string {
	cache := strings.TrimSpace(task.Profile.Resources.CacheDirectory)
	if cache == "" || filepath.IsAbs(cache) {
		return cache
	}
	return filepath.Join(task.WorktreePath, cache)
}

// agentTaskEnvironment passes only configured nonsecret values plus TanGit's
// stable operation identity. Reserved identity values cannot be overridden by
// a profile, even if a task was created outside the desktop UI.
func agentTaskEnvironment(task agentwork.Task, requestPath, operationID string) []string {
	values := map[string]string{}
	for key, value := range task.Profile.Environment {
		if strings.TrimSpace(key) == "" || strings.ContainsAny(key, "=\x00") || strings.ContainsRune(value, '\x00') {
			continue
		}
		values[key] = value
	}
	values["TANGIT_AGENT_TASK_ID"] = task.ID
	values["TANGIT_AGENT_OPERATION_ID"] = operationID
	values["TANGIT_AGENT_WORKTREE"] = task.WorktreePath
	values["TANGIT_AGENT_REQUEST_FILE"] = requestPath
	if port := task.Profile.Resources.DevelopmentPort; port > 0 {
		values["TANGIT_AGENT_PORT"] = strconv.Itoa(port)
	}
	if database := task.Profile.Resources.DatabaseName; database != "" {
		values["TANGIT_AGENT_DATABASE"] = database
	}
	if cache := agentTaskCacheDirectory(task); cache != "" {
		values["TANGIT_AGENT_CACHE_DIR"] = cache
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	environment := make([]string, 0, len(keys))
	for _, key := range keys {
		environment = append(environment, key+"="+values[key])
	}
	return environment
}
