#!/usr/bin/env bash
set -uo pipefail

CLI_BIN=${CLI_BIN:-/CLIProxyAPI/CLIProxyAPI}
MANAGER_BIN=${MANAGER_BIN:-/CLIProxyAPI/cpa-manager-plus}

export CPA_MANAGER_PLUS_URL=${CPA_MANAGER_PLUS_URL:-http://127.0.0.1:18317}
export CPA_MANAGER_INTEGRATED=${CPA_MANAGER_INTEGRATED:-true}
export HTTP_ADDR=${HTTP_ADDR:-127.0.0.1:18317}
export CPA_UPSTREAM_URL=${CPA_UPSTREAM_URL:-http://127.0.0.1:8317}
export USAGE_DATA_DIR=${USAGE_DATA_DIR:-/data}
export USAGE_DB_PATH=${USAGE_DB_PATH:-${USAGE_DATA_DIR}/usage.sqlite}
export CPA_MANAGER_DATA_KEY_PATH=${CPA_MANAGER_DATA_KEY_PATH:-${USAGE_DATA_DIR}/data.key}

if [[ ${CPA_MANAGER_INTEGRATED,,} == "true" && -n ${CPA_MANAGEMENT_KEY:-} ]]; then
  export CPA_MANAGER_ADMIN_KEY=${CPA_MANAGEMENT_KEY}
fi

"${CLI_BIN}" "$@" &
cli_pid=$!
"${MANAGER_BIN}" &
manager_pid=$!

terminate() {
  kill -TERM "${cli_pid}" "${manager_pid}" 2>/dev/null || true
}
trap terminate TERM INT

set +e
wait -n "${cli_pid}" "${manager_pid}"
status=$?
terminate
wait "${cli_pid}" 2>/dev/null
wait "${manager_pid}" 2>/dev/null
exit "${status}"
