#!/bin/sh
# Build and execute against an isolated Docker daemon reached by SSH context.
set -eu
cd "$(dirname "$0")/../.."
context=${1:-gov-remote}
endpoint=$(docker context inspect "$context" --format '{{.Endpoints.docker.Host}}')
case "$endpoint" in ssh://*) host=${endpoint#ssh://};; *) echo 'Choose an SSH remote Docker context.' >&2; exit 1;; esac
remote_dir=$(ssh "$host" 'mktemp -d /tmp/attic-ui-e2e.XXXXXX')
project=attic-ui-e2e-$(date +%s)
cleanup() {
  results="ui/test-results/$project"
  mkdir -p "$results"
  ssh "$host" "docker compose -p '$project' -f '$remote_dir/compose.ui-e2e.yaml' cp tests:/src/ui/test-results '$remote_dir/results' >/dev/null 2>&1 && tar -czf - -C '$remote_dir/results' ." |
    tar -xzf - -C "$results" || true
  ssh "$host" "docker compose -p '$project' -f '$remote_dir/compose.ui-e2e.yaml' down -v --remove-orphans; rm -rf '$remote_dir'"
}
trap cleanup EXIT HUP INT TERM
# Explicit allowlist excludes operator credentials, local dependencies and designs.
COPYFILE_DISABLE=1 tar --exclude='._*' --exclude=node_modules --exclude=dist --exclude=test-results --exclude=playwright-report -czf - Dockerfile .dockerignore compose.ui-e2e.yaml go.mod go.sum cmd internal migrations package.json pnpm-lock.yaml pnpm-workspace.yaml ui |
  ssh "$host" "tar -xzf - -C '$remote_dir'"
ssh "$host" "cd '$remote_dir' && docker compose -p '$project' -f compose.ui-e2e.yaml up --build --abort-on-container-exit --exit-code-from tests"
