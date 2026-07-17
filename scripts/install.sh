#!/usr/bin/env bash
# install.sh - Install ContextMatrix config and skill files into the user config directory.
#
# Usage:
#   scripts/install.sh                            # Fresh install: create config dir, copy config.yaml and workflow-skills/
#   scripts/install.sh --update-workflow-skills   # Only update the workflow-skills/ directory (safe to re-run)
#   scripts/install.sh --force                    # Overwrite config.yaml even if it already exists
#   scripts/install.sh --update-workflow-skills --force  # Combinable (--force is ignored for --update-workflow-skills mode)
#
# Config directory resolution (XDG):
#   $XDG_CONFIG_HOME/contextmatrix     if XDG_CONFIG_HOME is set
#   ~/.config/contextmatrix            otherwise
#
# Workflow skills install to <config-dir>/workflow-skills/ (config key
# workflow_skills_dir; env CONTEXTMATRIX_WORKFLOW_SKILLS_DIR) and refresh with
# --update-workflow-skills.

set -euo pipefail

# ---------------------------------------------------------------------------
# Locate the repo root (the directory that contains this script's parent).
# scripts/install.sh lives one level below the repo root, so REPO_ROOT is
# the parent of the directory that contains this script.
# ---------------------------------------------------------------------------
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

# ---------------------------------------------------------------------------
# Resolve config directory via XDG spec.
# ---------------------------------------------------------------------------
if [[ -n "${XDG_CONFIG_HOME:-}" ]]; then
    CONFIG_DIR="${XDG_CONFIG_HOME}/contextmatrix"
else
    CONFIG_DIR="${HOME}/.config/contextmatrix"
fi

# ---------------------------------------------------------------------------
# Parse arguments.
# ---------------------------------------------------------------------------
UPDATE_WORKFLOW_SKILLS=false
FORCE=false

for arg in "$@"; do
    case "${arg}" in
        --update-workflow-skills)
            UPDATE_WORKFLOW_SKILLS=true
            ;;
        --force)
            FORCE=true
            ;;
        *)
            echo "Unknown argument: ${arg}" >&2
            echo "Usage: $0 [--update-workflow-skills] [--force]" >&2
            exit 1
            ;;
    esac
done

# ---------------------------------------------------------------------------
# Helpers.
# ---------------------------------------------------------------------------
info()    { echo "[install] $*"; }
skipped() { echo "[install] SKIP   $*"; }
copied()  { echo "[install] COPIED $*"; }

# ---------------------------------------------------------------------------
# Ensure config directory exists.
# ---------------------------------------------------------------------------
if [[ ! -d "${CONFIG_DIR}" ]]; then
    mkdir -p "${CONFIG_DIR}"
    info "Created config directory: ${CONFIG_DIR}"
else
    info "Config directory already exists: ${CONFIG_DIR}"
fi

# ---------------------------------------------------------------------------
# Copy workflow skills (always done, whether default or --update-workflow-skills).
# ---------------------------------------------------------------------------
WORKFLOW_SKILLS_SRC="${REPO_ROOT}/workflow-skills"
WORKFLOW_SKILLS_DST="${CONFIG_DIR}/workflow-skills"

if [[ ! -d "${WORKFLOW_SKILLS_SRC}" ]]; then
    echo "[install] ERROR: workflow skills source not found at ${WORKFLOW_SKILLS_SRC}" >&2
    exit 1
fi

mkdir -p "${WORKFLOW_SKILLS_DST}"

# Copy each skill file individually so we report per-file.
while IFS= read -r -d '' skill_file; do
    rel="${skill_file#"${WORKFLOW_SKILLS_SRC}"/}"
    dest="${WORKFLOW_SKILLS_DST}/${rel}"
    dest_dir="$(dirname "${dest}")"
    mkdir -p "${dest_dir}"
    cp "${skill_file}" "${dest}"
    copied "workflow-skills/${rel} → ${dest}"
done < <(find "${WORKFLOW_SKILLS_SRC}" -type f -print0)

# ---------------------------------------------------------------------------
# If --update-workflow-skills was requested, stop here.
# ---------------------------------------------------------------------------
if [[ "${UPDATE_WORKFLOW_SKILLS}" == true ]]; then
    info "Workflow skills updated. config.yaml was not touched."
    exit 0
fi

# ---------------------------------------------------------------------------
# Copy config.yaml.example → config.yaml (skip if exists unless --force).
# ---------------------------------------------------------------------------
CONFIG_EXAMPLE="${REPO_ROOT}/config.yaml.example"
CONFIG_DST="${CONFIG_DIR}/config.yaml"

if [[ ! -f "${CONFIG_EXAMPLE}" ]]; then
    echo "[install] ERROR: config.yaml.example not found at ${CONFIG_EXAMPLE}" >&2
    exit 1
fi

if [[ -f "${CONFIG_DST}" && "${FORCE}" == false ]]; then
    skipped "config.yaml already exists at ${CONFIG_DST} (use --force to overwrite)"
else
    cp "${CONFIG_EXAMPLE}" "${CONFIG_DST}"
    copied "config.yaml.example → ${CONFIG_DST}"
fi

info "Done. Config directory: ${CONFIG_DIR}"
