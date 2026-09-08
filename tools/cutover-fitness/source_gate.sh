#!/bin/sh
set -eu

ani_repo=${ANI_REPO:?ANI_REPO is required}
iam_repo=${IAM_REPO:?IAM_REPO is required}
output=${OUTPUT_DIR:?OUTPUT_DIR is required}
base=56a5f0b493c8404a024a92647d93f2ba2f7daf35
overlay_source=804db51a5f93605f9bbd4ac407f0489ecb1d187c
iam_product=a56a332834967603eb47a3824727982032bc4f5e
expected_overlay=542084f3e06be1d454cd99eb0114a76b8e06a52f9cbf5a9d661e749fb678edc2
expected_projected=28eb0508c93aab28e86e044a88ada9e19bf16830

case "$output" in /tmp/ani-cf01-20260908t105253z-*) ;; *) echo "unsafe OUTPUT_DIR" >&2; exit 1;; esac
mkdir -p "$output/current" "$output/target" "$output/iam"
git -C "$ani_repo" archive "$base" | tar -x -C "$output/current"
git -C "$ani_repo" archive "$base" | tar -x -C "$output/target"
git -C "$iam_repo" archive "$iam_product" | tar -x -C "$output/iam"
git -C "$ani_repo" -c core.quotePath=true diff --binary --full-index --no-ext-diff "$base" "$overlay_source" -- >"$output/target-overlay.patch"
actual_overlay=$(sha256sum "$output/target-overlay.patch" | awk '{print $1}')
test "$actual_overlay" = "$expected_overlay"
(cd "$output/target" && git apply "$output/target-overlay.patch")
index="$output/projected.index"
objects="$output/objects"
mkdir -p "$objects"
git_dir=$(git -C "$ani_repo" rev-parse --absolute-git-dir)
alternates="$git_dir/objects"
GIT_OBJECT_DIRECTORY="$objects" GIT_ALTERNATE_OBJECT_DIRECTORIES="$alternates" GIT_INDEX_FILE="$index" git -C "$ani_repo" read-tree "$base^{tree}"
GIT_OBJECT_DIRECTORY="$objects" GIT_ALTERNATE_OBJECT_DIRECTORIES="$alternates" GIT_INDEX_FILE="$index" git -C "$ani_repo" --work-tree="$output/target" add -A
actual_projected=$(GIT_OBJECT_DIRECTORY="$objects" GIT_ALTERNATE_OBJECT_DIRECTORIES="$alternates" GIT_INDEX_FILE="$index" git -C "$ani_repo" write-tree)
test "$actual_projected" = "$expected_projected"
printf '{"ani_base":"%s","overlay_sha256":"%s","projected_tree":"%s","ani_iam_product":"%s"}\n' \
  "$base" "$actual_overlay" "$actual_projected" "$iam_product"
