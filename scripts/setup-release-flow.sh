#!/usr/bin/env bash
set -euo pipefail

repo="${REPO:-Kirari04/videocms}"
checks=("promotion-source" "go" "frontend" "docs" "docker-build")

require() {
	if ! command -v "$1" >/dev/null 2>&1; then
		echo "Missing required command: $1" >&2
		exit 1
	fi
}

ensure_branch() {
	local branch="$1"
	local sha="$2"

	if gh api "repos/${repo}/git/ref/heads/${branch}" >/dev/null 2>&1; then
		echo "Branch ${branch} already exists."
		return
	fi

	echo "Creating ${branch} from master (${sha})."
	gh api \
		-X POST \
		"repos/${repo}/git/refs" \
		-f ref="refs/heads/${branch}" \
		-f sha="${sha}" \
		>/dev/null
}

protect_branch() {
	local branch="$1"
	local contexts_json

	contexts_json="$(printf '%s\n' "${checks[@]}" | jq -R . | jq -s .)"

	echo "Configuring protection for ${branch}."
	jq -n \
		--argjson contexts "${contexts_json}" \
		'{
			required_status_checks: {
				strict: true,
				contexts: $contexts
			},
			enforce_admins: true,
			required_pull_request_reviews: {
				dismiss_stale_reviews: true,
				require_code_owner_reviews: false,
				required_approving_review_count: 1,
				require_last_push_approval: false
			},
			restrictions: null,
			required_linear_history: false,
			allow_force_pushes: false,
			allow_deletions: false,
			block_creations: false,
			required_conversation_resolution: true,
			lock_branch: false,
			allow_fork_syncing: true
		}' |
		gh api \
			-X PUT \
			"repos/${repo}/branches/${branch}/protection" \
			--input - \
			>/dev/null
}

protect_release_tags() {
	local name="Protect release version tags"
	local existing_id
	local body

	body="$(mktemp)"
	trap 'rm -f "${body}"' RETURN

	jq -n \
		--arg name "${name}" \
		'{
			name: $name,
			target: "tag",
			enforcement: "active",
			conditions: {
				ref_name: {
					include: ["refs/tags/v*"],
					exclude: []
				}
			},
			rules: [
				{type: "deletion"},
				{type: "non_fast_forward"}
			]
		}' > "${body}"

	existing_id="$(gh api "repos/${repo}/rulesets" --jq ".[] | select(.name == \"${name}\") | .id" | head -n1 || true)"

	if [ -n "${existing_id}" ]; then
		echo "Updating tag ruleset ${name}."
		gh api -X PUT "repos/${repo}/rulesets/${existing_id}" --input "${body}" >/dev/null
	else
		echo "Creating tag ruleset ${name}."
		gh api -X POST "repos/${repo}/rulesets" --input "${body}" >/dev/null
	fi
}

require gh
require jq

master_sha="$(gh api "repos/${repo}/git/ref/heads/master" --jq .object.sha)"

ensure_branch dev "${master_sha}"
ensure_branch staging "${master_sha}"

echo "Setting default branch to dev."
gh api -X PATCH "repos/${repo}" -f default_branch=dev >/dev/null

protect_branch dev
protect_branch staging
protect_branch master
protect_release_tags

cat <<EOF
Release flow configured for ${repo}.

Branches:
- dev: integration branch
- staging: release testing branch
- master: release-only branch

Promotion source rules are enforced by the CI workflow:
- dev -> staging
- staging -> master
EOF
