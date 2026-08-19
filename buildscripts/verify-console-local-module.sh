#!/usr/bin/env bash
#
# Verify that github.com/minio/console is embedded as a local module under
# console/ with an auditable provenance record and the repository-wide Go
# toolchain policy applied.

set -euo pipefail

readonly repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
readonly console_dir="$repo_root/console"
readonly console_mod="$console_dir/go.mod"
readonly root_mod="$repo_root/go.mod"

readonly expected_module="github.com/minio/console"
readonly expected_go="1.25.0"
readonly expected_toolchain="go1.26.6"
readonly expected_replace="replace github.com/minio/console => ./console"

readonly required_files=(
	"LICENSE"
	"NOTICE"
	"CREDITS"
	"UPSTREAM-SOURCE.md"
)

problems=()

fail() {
	problems+=("$1")
}

if [[ ! -f "$console_mod" ]]; then
	fail "missing $console_mod; the Console module is not embedded under console/"
else
	actual_module="$(awk '$1 == "module" { print $2; exit }' "$console_mod")"
	if [[ "$actual_module" != "$expected_module" ]]; then
		fail "console/go.mod module must be $expected_module, found ${actual_module:-<none>}"
	fi

	actual_go="$(awk '$1 == "go" { print $2; exit }' "$console_mod")"
	if [[ "$actual_go" != "$expected_go" ]]; then
		fail "console/go.mod go directive must be $expected_go, found ${actual_go:-<none>}"
	fi

	actual_toolchain="$(awk '$1 == "toolchain" { print $2; exit }' "$console_mod")"
	if [[ "$actual_toolchain" != "$expected_toolchain" ]]; then
		fail "console/go.mod toolchain directive must be $expected_toolchain, found ${actual_toolchain:-<none>}"
	fi
fi

if ! grep -Fq "$expected_replace" "$root_mod"; then
	fail "root go.mod must contain: $expected_replace"
fi

for name in "${required_files[@]}"; do
	if [[ ! -f "$console_dir/$name" ]]; then
		fail "missing provenance or license file: console/$name"
	fi
done

# The module must resolve to the in-repository directory, not the module cache.
if resolved_dir="$(cd "$repo_root" && go list -m -f '{{.Dir}}' "$expected_module" 2>/dev/null)"; then
	if [[ "$resolved_dir" != "$console_dir" ]]; then
		fail "go list resolves $expected_module to $resolved_dir, expected $console_dir"
	fi
else
	fail "go list could not resolve $expected_module from the repository root"
fi

# Transfer artifacts must not leak into the vendored source.
if [[ -d "$console_dir" ]]; then
	# Only inspect committed sources. Local build artifacts such as
	# console/web-app/node_modules/ are git-ignored and must not be scanned.
	tracked_count="$(git -C "$repo_root" ls-files -- console | wc -l | tr -d ' ')"
	if [[ "$tracked_count" -eq 0 ]]; then
		fail "no tracked files found under console/; the module does not appear to be committed"
	else
		leaked="$(git -C "$repo_root" ls-files -z -- console |
			xargs -0 grep -IlE '/pkg/mod/|/private/tmp/|/var/folders/|/Users/[a-z]' 2>/dev/null || true)"
		if [[ -n "$leaked" ]]; then
			fail "module cache or temporary directory paths leaked into console/: $(echo "$leaked" | tr '\n' ' ')"
		fi

		# Module cache files are read-only; the embedded copy must be writable source.
		readonly_files="$(find "$console_dir" -name node_modules -prune -o -name .yarn -prune -o \
			-type f ! -perm -u+w -print 2>/dev/null | head -1)"
		if [[ -n "$readonly_files" ]]; then
			fail "console/ contains read-only files carried over from the module cache: $readonly_files"
		fi
	fi
fi

if ((${#problems[@]} > 0)); then
	echo "Console local module verification failed:" >&2
	for problem in "${problems[@]}"; do
		echo "- $problem" >&2
	done
	exit 1
fi

echo "Console local module verification passed ($expected_module => ./console)"
