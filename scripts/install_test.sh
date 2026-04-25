#!/usr/bin/env bash
# install_test.sh — Shell-based tests for scripts/install.sh.
#
# Runs in a temporary directory with a mocked XDG_CONFIG_HOME.
# All tests are self-contained and clean up after themselves.
#
# Usage:
#   scripts/install_test.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
INSTALL_SCRIPT="${SCRIPT_DIR}/install.sh"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

# ---------------------------------------------------------------------------
# Test framework helpers.
# ---------------------------------------------------------------------------
PASS=0
FAIL=0

pass() {
    PASS=$((PASS + 1))
    echo "  PASS: $*"
}

fail() {
    FAIL=$((FAIL + 1))
    echo "  FAIL: $*"
}

assert_exists() {
    local path="$1"
    local desc="${2:-${path}}"
    if [[ -e "${path}" ]]; then
        pass "${desc} exists"
    else
        fail "${desc} does not exist (expected: ${path})"
    fi
}

assert_not_exists() {
    local path="$1"
    local desc="${2:-${path}}"
    if [[ ! -e "${path}" ]]; then
        pass "${desc} does not exist (as expected)"
    else
        fail "${desc} should not exist but does (path: ${path})"
    fi
}

assert_file_contains() {
    local path="$1"
    local pattern="$2"
    local desc="${3:-contains '${pattern}'}"
    if grep -q "${pattern}" "${path}" 2>/dev/null; then
        pass "${desc}"
    else
        fail "${desc} — pattern '${pattern}' not found in ${path}"
    fi
}

assert_file_not_contains() {
    local path="$1"
    local pattern="$2"
    local desc="${3:-does not contain '${pattern}'}"
    if ! grep -q "${pattern}" "${path}" 2>/dev/null; then
        pass "${desc}"
    else
        fail "${desc} — pattern '${pattern}' unexpectedly found in ${path}"
    fi
}

# ---------------------------------------------------------------------------
# Setup / teardown helpers.
# ---------------------------------------------------------------------------
TMPDIR_BASE=""

setup() {
    TMPDIR_BASE="$(mktemp -d)"
    export XDG_CONFIG_HOME="${TMPDIR_BASE}/xdg_config"
}

teardown() {
    if [[ -n "${TMPDIR_BASE}" && -d "${TMPDIR_BASE}" ]]; then
        rm -rf "${TMPDIR_BASE}"
    fi
    TMPDIR_BASE=""
}

CONFIG_DIR() {
    echo "${XDG_CONFIG_HOME}/contextmatrix"
}

# ---------------------------------------------------------------------------
# Tests
# ---------------------------------------------------------------------------

run_test_fresh_install() {
    echo ""
    echo "Test: fresh install creates config.yaml and workflow-skills/"
    setup

    "${INSTALL_SCRIPT}"

    local config_dir
    config_dir="$(CONFIG_DIR)"

    assert_exists "${config_dir}" "config dir"
    assert_exists "${config_dir}/config.yaml" "config.yaml"
    assert_exists "${config_dir}/workflow-skills" "workflow-skills dir"

    # Verify at least one skill file was copied.
    local skill_count
    skill_count=$(find "${config_dir}/workflow-skills" -type f | wc -l)
    if [[ "${skill_count}" -gt 0 ]]; then
        pass "workflow-skills directory contains ${skill_count} file(s)"
    else
        fail "workflow-skills directory is empty after fresh install"
    fi

    # config.yaml should contain content from config.yaml.example.
    assert_file_contains "${config_dir}/config.yaml" "boards_dir" "config.yaml has boards_dir field"

    teardown
}

run_test_rerun_skips_config() {
    echo ""
    echo "Test: re-run skips existing config.yaml"
    setup

    # First install.
    "${INSTALL_SCRIPT}"

    local config_dir
    config_dir="$(CONFIG_DIR)"

    # Overwrite config.yaml with a sentinel value.
    echo "# SENTINEL_VALUE_DO_NOT_OVERWRITE" > "${config_dir}/config.yaml"

    # Second install — should not overwrite.
    "${INSTALL_SCRIPT}"

    assert_file_contains "${config_dir}/config.yaml" "SENTINEL_VALUE_DO_NOT_OVERWRITE" \
        "config.yaml still contains sentinel after re-run (was not overwritten)"

    teardown
}

run_test_update_skills_only_touches_skills() {
    echo ""
    echo "Test: --update-workflow-skills only copies workflow skills, does not touch config.yaml"
    setup

    # First do a full install to establish the config dir.
    "${INSTALL_SCRIPT}"

    local config_dir
    config_dir="$(CONFIG_DIR)"

    # Write a sentinel into config.yaml.
    echo "# SENTINEL_SHOULD_REMAIN" > "${config_dir}/config.yaml"

    # Remove workflow-skills dir to confirm it gets recreated.
    rm -rf "${config_dir}/workflow-skills"
    assert_not_exists "${config_dir}/workflow-skills" "workflow-skills dir removed before --update-workflow-skills"

    # Run with --update-workflow-skills.
    "${INSTALL_SCRIPT}" --update-workflow-skills

    # Workflow skills should be back.
    assert_exists "${config_dir}/workflow-skills" "workflow-skills dir recreated by --update-workflow-skills"

    local skill_count
    skill_count=$(find "${config_dir}/workflow-skills" -type f | wc -l)
    if [[ "${skill_count}" -gt 0 ]]; then
        pass "--update-workflow-skills restored ${skill_count} skill file(s)"
    else
        fail "--update-workflow-skills left workflow-skills directory empty"
    fi

    # config.yaml must be untouched.
    assert_file_contains "${config_dir}/config.yaml" "SENTINEL_SHOULD_REMAIN" \
        "config.yaml untouched by --update-workflow-skills"

    teardown
}

run_test_force_overwrites_config() {
    echo ""
    echo "Test: --force overwrites existing config.yaml"
    setup

    # First install.
    "${INSTALL_SCRIPT}"

    local config_dir
    config_dir="$(CONFIG_DIR)"

    # Write a sentinel.
    echo "# OLD_SENTINEL" > "${config_dir}/config.yaml"

    # Second install with --force.
    "${INSTALL_SCRIPT}" --force

    # config.yaml should now be the example file, not the sentinel.
    assert_file_not_contains "${config_dir}/config.yaml" "OLD_SENTINEL" \
        "config.yaml sentinel was replaced by --force"
    assert_file_contains "${config_dir}/config.yaml" "boards_dir" \
        "config.yaml contains example content after --force"

    teardown
}

run_test_xdg_fallback() {
    echo ""
    echo "Test: unset XDG_CONFIG_HOME uses ~/.config/contextmatrix"
    setup

    # Unset XDG_CONFIG_HOME and use HOME pointing to temp dir.
    local saved_xdg="${XDG_CONFIG_HOME}"
    local saved_home="${HOME}"
    unset XDG_CONFIG_HOME
    export HOME="${TMPDIR_BASE}/fake_home"
    mkdir -p "${HOME}"

    "${INSTALL_SCRIPT}"

    assert_exists "${HOME}/.config/contextmatrix/config.yaml" "config.yaml in HOME fallback path"
    assert_exists "${HOME}/.config/contextmatrix/workflow-skills" "workflow-skills in HOME fallback path"

    # Restore.
    export XDG_CONFIG_HOME="${saved_xdg}"
    export HOME="${saved_home}"

    teardown
}

run_test_unknown_flag_exits_nonzero() {
    echo ""
    echo "Test: unknown flag causes non-zero exit"
    setup

    local exit_code=0
    "${INSTALL_SCRIPT}" --unknown-flag 2>/dev/null || exit_code=$?

    if [[ "${exit_code}" -ne 0 ]]; then
        pass "unknown flag exits non-zero (exit code: ${exit_code})"
    else
        fail "unknown flag should exit non-zero but exited 0"
    fi

    teardown
}

# ---------------------------------------------------------------------------
# Main: run all tests.
# ---------------------------------------------------------------------------
echo "=== ContextMatrix install.sh tests ==="
echo "Repo root:     ${REPO_ROOT}"
echo "Install script: ${INSTALL_SCRIPT}"

run_test_fresh_install
run_test_rerun_skips_config
run_test_update_skills_only_touches_skills
run_test_force_overwrites_config
run_test_xdg_fallback
run_test_unknown_flag_exits_nonzero

echo ""
echo "=== Results: ${PASS} passed, ${FAIL} failed ==="

if [[ "${FAIL}" -gt 0 ]]; then
    exit 1
fi
