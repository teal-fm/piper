#!/usr/bin/env bash
set -euo pipefail

mode=${1:?Expected main, pr, or release}
image=${PIPER_TANGLED_IMAGE:?Set PIPER_TANGLED_IMAGE to a dedicated Tangled image repository}
revision=$(git rev-parse HEAD)
channel=main
moving_tag=
source_ref=

case "$mode" in
  main)
    test "${TANGLED_PIPELINE_KIND:-}" = push
    test "${TANGLED_REF:-}" = refs/heads/main
    test "$revision" = "${TANGLED_SHA:-}"
    fixed_tag="sha-${revision:0:7}"
    moving_tag=main
    source_ref=refs/heads/main
    ;;
  pr)
    test "${TANGLED_PIPELINE_KIND:-}" = pull_request
    test "$revision" = "${TANGLED_PR_SOURCE_SHA:-}"
    git check-ref-format "refs/heads/${TANGLED_PR_SOURCE_BRANCH:?}"
    git check-ref-format "refs/heads/${TANGLED_PR_TARGET_BRANCH:?}"
    # Spindle does not provide a PR number. Hash the branch pair for a stable,
    # registry-safe tag, including source repository identity to avoid clashes.
    branch_key=$(printf '%s\n' "${TANGLED_REPO_URL:?}" "$TANGLED_PR_SOURCE_BRANCH" "$TANGLED_PR_TARGET_BRANCH" | sha256sum)
    moving_tag="pr-${branch_key:0:12}"
    fixed_tag="$moving_tag-${revision:0:7}"
    source_ref="refs/heads/$TANGLED_PR_SOURCE_BRANCH"
    ;;
  release)
    test "${TANGLED_PIPELINE_KIND:-}" = push
    test "${TANGLED_REF_TYPE:-}" = tag
    version=$(node -p 'JSON.parse(require("fs").readFileSync("package.json", "utf8")).version')
    [[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]
    test "${TANGLED_REF_NAME:-}" = "v$version"
    test "${TANGLED_REF:-}" = "refs/tags/v$version"
    # Annotated tag pushes may carry the tag object SHA; resolve to its commit.
    test "$revision" = "$(git rev-parse "${TANGLED_SHA:?}^{commit}")"
    fixed_tag="$version"
    channel=release
    ;;
  *) printf 'Unknown image mode: %s\n' "$mode" >&2; exit 1 ;;
esac

# Main/release publication requires configured secrets. Fork PRs receive no
# secrets from Spindle and still build both architectures as a CI check.
if [ "$mode" != pr ]; then
  : "${TANGLED_REGISTRY_USERNAME:?Configure the registry username in Tangled secrets}"
  : "${TANGLED_REGISTRY_TOKEN:?Configure a registry token in Tangled secrets}"
fi

workdir=$(mktemp -d)
trap 'rm -rf "$workdir"' EXIT

docker run --privileged --rm tonistiigi/binfmt --install arm64,amd64
docker buildx create --name piper-spindle --driver docker-container --use
docker buildx inspect --bootstrap
docker buildx build --platform linux/amd64,linux/arm64 \
  --build-arg "PIPER_BUILD_CHANNEL=$channel" \
  --build-arg "PIPER_BUILD_REVISION=$revision" \
  --label "org.opencontainers.image.source=https://tangled.org/teal.fm/piper" \
  --label "org.opencontainers.image.revision=$revision" \
  --output "type=oci,dest=$workdir/image.tar" .

if [ -z "${TANGLED_REGISTRY_USERNAME:-}" ] || [ -z "${TANGLED_REGISTRY_TOKEN:-}" ]; then
  printf 'Both architectures built. Registry publishing is unavailable without trusted pipeline secrets.\n'
  exit 0
fi

# Recheck the source after the build so an older run does not normally replace
# a newer branch image. Spindle has no workflow concurrency groups; a push during
# the final copy can still race. Commit-specific tags remain available.
if [ -n "$source_ref" ]; then
  current=$(git ls-remote --exit-code origin "$source_ref" | cut -f1)
  if [ "$current" != "$revision" ]; then
    printf 'Source branch advanced; skipping publication of %s.\n' "$revision"
    exit 0
  fi
fi

registry=${image%%/*}
printf '%s' "$TANGLED_REGISTRY_TOKEN" | skopeo login \
  --authfile "$workdir/auth.json" --username "$TANGLED_REGISTRY_USERNAME" \
  --password-stdin "$registry"

publish() {
  skopeo copy --all --authfile "$workdir/auth.json" \
    "oci-archive:$workdir/image.tar" "docker://$image:$1"
  printf 'Download: docker pull %s:%s\n' "$image" "$1"
}

publish "$fixed_tag"
if [ -n "$moving_tag" ]; then
  publish "$moving_tag"
fi
if [ "$mode" = release ]; then
  publish "v$version"
  # Never let a replay of an older release move latest backwards. Tag-only
  # mirror updates still publish the exact version even if main has advanced.
  current=$(git ls-remote --exit-code origin refs/heads/main | cut -f1)
  if [ "$current" = "$revision" ]; then
    publish latest
  fi
fi
