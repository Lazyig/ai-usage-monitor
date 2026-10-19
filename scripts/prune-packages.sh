#!/bin/sh
# Delete old versions of the ai-usage-agent generic package, keeping the newest $KEEP
# (default 10). Never deletes "latest" or release tags (v*). DRY_RUN=1 only prints.
# Runs inside GitLab CI (uses CI_JOB_TOKEN, CI_API_V4_URL, CI_PROJECT_ID); locally set
# GITLAB_TOKEN instead.
set -eu
KEEP="${KEEP:-10}"
DRY_RUN="${DRY_RUN:-0}"
[ "$KEEP" -ge 1 ] || { echo "KEEP must be >= 1" >&2; exit 2; }
base="$CI_API_V4_URL/projects/$CI_PROJECT_ID/packages"
if [ -n "${GITLAB_TOKEN:-}" ]; then
  auth="PRIVATE-TOKEN: $GITLAB_TOKEN"
else
  auth="JOB-TOKEN: $CI_JOB_TOKEN"
fi
tab=$(printf '\t')
list=$(mktemp)
del=$(mktemp)
trap 'rm -f "$list" "$del"' EXIT

# newest first; the package_name filter is fuzzy, so match the exact name again below
page=1
while :; do
  out=$(curl -fsS --header "$auth" \
    "$base?package_type=generic&package_name=ai-usage-agent&order_by=created_at&sort=desc&per_page=100&page=$page")
  [ "$(echo "$out" | jq length)" -gt 0 ] || break
  echo "$out" | jq -r '.[] | select(.name == "ai-usage-agent") | [.id, .version] | @tsv' >> "$list"
  page=$((page + 1))
done

awk -F"$tab" -v keep="$KEEP" '$2 != "latest" && $2 !~ /^v[0-9]/ { n++; if (n > keep) print $1 "\t" $2 }' \
  "$list" > "$del"

echo "versions: $(wc -l < "$list" | tr -d ' '), to delete: $(wc -l < "$del" | tr -d ' ') (keep newest $KEEP)"
while IFS="$tab" read -r id version; do
  if [ "$DRY_RUN" = 1 ]; then
    echo "would delete $version (id $id)"
    continue
  fi
  echo "deleting $version (id $id)"
  curl -fsS -X DELETE --header "$auth" "$base/$id" > /dev/null
done < "$del"
