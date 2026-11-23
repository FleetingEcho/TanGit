package app

import (
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/FleetingEcho/lazygitGo/internal/agentwork"
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

const agentEnvironmentPrefix = "LAZYGITGO_AGENT_"
const legacyAgentEnvironmentPrefix = "TANGIT_AGENT_"

func isReservedAgentEnvironment(key string) bool {
	return strings.HasPrefix(key, agentEnvironmentPrefix) || strings.HasPrefix(key, legacyAgentEnvironmentPrefix)
}

// agentTaskEnvironment passes only configured nonsecret values plus lazygitGo's
// stable operation identity. Reserved identity values cannot be overridden by
// a profile, even if a task was created outside the desktop UI.
func agentTaskEnvironment(task agentwork.Task, requestPath, operationID string) []string {
	values := map[string]string{}
	for key, value := range task.Profile.Environment {
		if isReservedAgentEnvironment(key) || strings.TrimSpace(key) == "" || strings.ContainsAny(key, "=\x00") || strings.ContainsRune(value, '\x00') {
			continue
		}
		values[key] = value
	}
	setIdentity := func(name, value string) {
		values[agentEnvironmentPrefix+name] = value
		values[legacyAgentEnvironmentPrefix+name] = value
	}
	setIdentity("TASK_ID", task.ID)
	setIdentity("OPERATION_ID", operationID)
	setIdentity("WORKTREE", task.WorktreePath)
	setIdentity("REQUEST_FILE", requestPath)
	if port := task.Profile.Resources.DevelopmentPort; port > 0 {
		setIdentity("PORT", strconv.Itoa(port))
	}
	if database := task.Profile.Resources.DatabaseName; database != "" {
		setIdentity("DATABASE", database)
	}
	if cache := agentTaskCacheDirectory(task); cache != "" {
		setIdentity("CACHE_DIR", cache)
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
