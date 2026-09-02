#!/bin/sh
# Upload every file in $DIR to the GitLab generic package registry of the current
# project, under package name ai-usage-agent, twice: as $VERSION and as "latest".
# Runs inside GitLab CI (uses CI_JOB_TOKEN, CI_API_V4_URL, CI_PROJECT_ID).
set -eu
DIR="${1:-dist}"
VERSION="${VERSION:?VERSION required}"
base="$CI_API_V4_URL/projects/$CI_PROJECT_ID/packages/generic/ai-usage-agent"
for f in "$DIR"/ai-usage-agent_* "$DIR"/manifest.json "$DIR"/install.sh "$DIR"/install.ps1; do
  [ -f "$f" ] || continue
  name=$(basename "$f")
  for v in "$VERSION" latest; do
    echo "uploading $name -> $v"
    curl -fsS --retry 3 --header "JOB-TOKEN: $CI_JOB_TOKEN" \
      --upload-file "$f" "$base/$v/$name?select=package_file" > /dev/null
  done
done
