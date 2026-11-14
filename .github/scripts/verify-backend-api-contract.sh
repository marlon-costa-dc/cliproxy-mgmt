#!/usr/bin/env bash
set -euo pipefail

if [ -z "${BACKEND_API_PREFIX:-}" ]; then
  echo "[!] CLIPROXY_BACKEND_API_PREFIX repository variable is required."
  exit 1
fi

panel_api_prefix="$(sed -n "s/.*MANAGEMENT_API_PREFIX = '\\([^']*\\)'.*/\\1/p" src/utils/constants.ts)"
if [ -z "${panel_api_prefix}" ]; then
  echo "[!] Cannot read MANAGEMENT_API_PREFIX from src/utils/constants.ts."
  exit 1
fi

if [ "${panel_api_prefix}" != "${BACKEND_API_PREFIX}" ]; then
  echo "[!] Refusing to continue: panel requires ${panel_api_prefix} but the deployed backend contract is ${BACKEND_API_PREFIX}."
  exit 1
fi

echo "[i] API contract matches: ${panel_api_prefix}."
