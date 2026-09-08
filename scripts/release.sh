#!/usr/bin/env bash
set -euo pipefail
tag=${GITHUB_REF_NAME:?Missing release tag}
[[ "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo 'Invalid stable version tag' >&2; exit 1; }
case "${1:-}" in
 upload)
   if prior=$(gh release view "$tag" --json isPrerelease --jq .isPrerelease); then
     if [[ "$prior" == true ]]; then gh release upload "$tag" dist/tgguard-linux-* dist/SHA256SUMS --clobber; fi
   else
     gh release create "$tag" dist/tgguard-linux-* dist/SHA256SUMS --prerelease --verify-tag --title "TG Guard $tag" --notes 'Linux amd64 / arm64 预编译程序及 SHA-256 校验文件。通过镜像验收后转为稳定版。'
   fi
   ;;
 promote)
   # List avoids treating a failed latest lookup as an empty release history.
   latest=$(gh release list --exclude-drafts --exclude-pre-releases --limit 100 --json tagName,isLatest --jq '.[] | select(.isLatest) | .tagName')
   promote=true
   if [[ -n "$latest" ]]; then
     [[ "$latest" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo 'Unexpected latest version' >&2; exit 1; }
     highest=$(printf '%s\n%s\n' "$latest" "$tag" | sort -V | tail -n 1)
     [[ "$highest" == "$tag" ]] || promote=false
   fi
   gh release edit "$tag" --prerelease=false --latest="$promote"
   ;;
 *) echo 'Usage: release.sh upload|promote' >&2; exit 1;;
esac
