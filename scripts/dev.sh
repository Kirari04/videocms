#!/usr/bin/env bash

set -Eeuo pipefail

repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
frontend_dir="${repo_root}/videocms-frontend"

frontend_host="${DEV_FRONTEND_HOST:-127.0.0.1}"
frontend_port="${DEV_FRONTEND_PORT:-3000}"
backend_host="${DEV_BACKEND_HOST:-127.0.0.1}"
backend_port="${DEV_BACKEND_PORT:-3001}"
backend_origin="http://${backend_host}:${backend_port}"
backend_url="${DEV_BACKEND_URL:-${backend_origin}}"
public_origin="${DEV_PUBLIC_ORIGIN:-http://${frontend_host}:${frontend_port}}"
media_path="${DEV_MEDIA_PATH:-${FolderVideoQualitysPub:-/videos/qualitys}}"
storage_encryption_key="${StorageEncryptionKey:-${DEV_STORAGE_ENCRYPTION_KEY:-}}"

backend_pid=""
frontend_pid=""

require_command() {
    if ! command -v "$1" >/dev/null 2>&1; then
        echo "Missing required command: $1" >&2
        exit 1
    fi
}

stop_processes() {
    local exit_code=$?

    trap - EXIT INT TERM

    for pid in "${frontend_pid}" "${backend_pid}"; do
        if [[ -n "${pid}" ]] && kill -0 "${pid}" 2>/dev/null; then
            kill -TERM "${pid}" 2>/dev/null || true
        fi
    done

    for pid in "${frontend_pid}" "${backend_pid}"; do
        if [[ -n "${pid}" ]]; then
            wait "${pid}" 2>/dev/null || true
        fi
    done

    exit "${exit_code}"
}

trap stop_processes EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

require_command go
require_command bun

if [[ ! -f "${frontend_dir}/package.json" ]]; then
    echo "Frontend submodule is missing. Run: git submodule update --init --recursive" >&2
    exit 1
fi

echo "Installing frontend dependencies..."
(
    cd "${frontend_dir}"
    bun install --frozen-lockfile
)

echo "Starting Go backend at ${backend_origin}"
(
    cd "${repo_root}"
    export Host="${backend_host}:${backend_port}"
    export FolderVideoQualitysPub="${media_path}"
    export StorageEncryptionKey="${storage_encryption_key}"
    exec go tool air serve:main
) &
backend_pid=$!

echo "Starting Nuxt frontend at ${public_origin}"
echo "Proxying /api and backend-owned routes to ${backend_url}"
(
    cd "${frontend_dir}"
    export NUXT_PUBLIC_API_URL="/api"
    export NUXT_PUBLIC_BASE_URL="${public_origin}"
    export NUXT_DEV_BACKEND_URL="${backend_url}"
    export NUXT_DEV_MEDIA_PATH="${media_path}"
    exec bun run dev --host "${frontend_host}" --port "${frontend_port}"
) &
frontend_pid=$!

set +e
wait -n "${backend_pid}" "${frontend_pid}"
child_status=$?
set -e

if ! kill -0 "${backend_pid}" 2>/dev/null; then
    echo "Go backend exited with status ${child_status}; stopping frontend." >&2
else
    echo "Nuxt frontend exited with status ${child_status}; stopping backend." >&2
fi

exit "${child_status}"
