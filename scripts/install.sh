#!/usr/bin/env bash
# install.sh — Install ContextMatrix config and skills files into the user config directory.
#
# Usage:
#   scripts/install.sh                 # Fresh install: create config dir, copy config.yaml and skills/
#   scripts/install.sh --update-skills # Only update the skills/ directory (safe to re-run)
#   scripts/install.sh --force         # Overwrite config.yaml even if it already exists
#   scripts/install.sh --update-skills --force  # Both flags may be combined (--force is ignored for --update-skills mode)
#
# Config directory resolution (XDG):
#   $XDG_CONFIG_HOME/contextmatrix     if XDG_CONFIG_HOME is set
#   ~/.config/contextmatrix            otherwise

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
UPDATE_SKILLS=false
FORCE=false

for arg in "$@"; do
    case "${arg}" in
        --update-skills)
            UPDATE_SKILLS=true
            ;;
        --force)
            FORCE=true
            ;;
        *)
            echo "Unknown argument: ${arg}" >&2
            echo "Usage: $0 [--update-skills] [--force]" >&2
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
# Copy skills/ directory (always done, whether default or --update-skills).
# ---------------------------------------------------------------------------
SKILLS_SRC="${REPO_ROOT}/skills"
SKILLS_DST="${CONFIG_DIR}/skills"

if [[ ! -d "${SKILLS_SRC}" ]]; then
    echo "[install] ERROR: skills directory not found at ${SKILLS_SRC}" >&2
    exit 1
fi

mkdir -p "${SKILLS_DST}"

# Copy each skill file individually so we report per-file.
while IFS= read -r -d '' skill_file; do
    rel="${skill_file#${SKILLS_SRC}/}"
    dest="${SKILLS_DST}/${rel}"
    dest_dir="$(dirname "${dest}")"
    mkdir -p "${dest_dir}"
    cp "${skill_file}" "${dest}"
    copied "skills/${rel} → ${dest}"
done < <(find "${SKILLS_SRC}" -type f -print0)

# ---------------------------------------------------------------------------
# If --update-skills was requested, stop here.
# ---------------------------------------------------------------------------
if [[ "${UPDATE_SKILLS}" == true ]]; then
    info "Skills updated. config.yaml was not touched."
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
