#!/usr/bin/env bash
set -euo pipefail

# -------- Config (override via env or flags) ----------
IMAGE_NAME="${IMAGE_NAME:-gothoom-build-env}"
TAG="${TAG:-$(date -u +%Y%m%d-%H%M%S)}"
DOCKERFILE="${DOCKERFILE:-Dockerfile}"
CONTEXT="${CONTEXT:-.}"
OUTDIR="${OUTDIR:-./dist-image}"
GZIP="${GZIP:-0}"          # 1 to gzip the tarball
PUSH="${PUSH:-0}"          # 1 to also docker push (if you have a registry)
LOAD_TEST="${LOAD_TEST:-0}"# 1 to test a load after saving

# -------- Flags ---------------------------------------
usage() {
  cat <<EOF
Usage: $(basename "$0") [options]

Options:
  -n, --name NAME         Image name (default: $IMAGE_NAME)
  -t, --tag TAG           Image tag  (default: $TAG)
  -f, --file DOCKERFILE   Dockerfile path (default: $DOCKERFILE)
  -c, --context PATH      Build context (default: $CONTEXT)
  -o, --outdir DIR        Output directory for tar/checksums (default: $OUTDIR)
  -z, --gzip              Gzip the saved tarball
  -p, --push              docker push after build (needs registry tag)
  -l, --load-test         Test loading the tar into local Docker after save
  -h, --help              Show this help

Environment overrides are also supported:
  IMAGE_NAME, TAG, DOCKERFILE, CONTEXT, OUTDIR, GZIP, PUSH, LOAD_TEST
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    -n|--name) IMAGE_NAME="$2"; shift 2 ;;
    -t|--tag) TAG="$2"; shift 2 ;;
    -f|--file) DOCKERFILE="$2"; shift 2 ;;
    -c|--context) CONTEXT="$2"; shift 2 ;;
    -o|--outdir) OUTDIR="$2"; shift 2 ;;
    -z|--gzip) GZIP=1; shift ;;
    -p|--push) PUSH=1; shift ;;
    -l|--load-test) LOAD_TEST=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown arg: $1" >&2; usage; exit 1 ;;
  esac
done

# -------- Build ---------------------------------------
echo ">> Building ${IMAGE_NAME}:${TAG} from ${DOCKERFILE} (context: ${CONTEXT})"
# Enable BuildKit if available; harmless if unsupported
export DOCKER_BUILDKIT=1

docker build \
  --file "${DOCKERFILE}" \
  --tag "${IMAGE_NAME}:${TAG}" \
  "${CONTEXT}"

# Optional push (if you've tagged with a registry, e.g. ghcr.io/user/repo:tag)
if [[ "$PUSH" == "1" ]]; then
  echo ">> Pushing ${IMAGE_NAME}:${TAG}"
  docker push "${IMAGE_NAME}:${TAG}"
fi

# -------- Save ----------------------------------------
mkdir -p "${OUTDIR}"
BASENAME="${IMAGE_NAME//\//_}-${TAG}"
TAR="${OUTDIR}/${BASENAME}.tar"

echo ">> Saving image to ${TAR}"
docker save -o "${TAR}" "${IMAGE_NAME}:${TAG}"

# Checksums
echo ">> Writing checksums"
( cd "${OUTDIR}" && sha256sum "$(basename "${TAR}")" > "${BASENAME}.tar.sha256" )

# Optional gzip
if [[ "$GZIP" == "1" ]]; then
  echo ">> Gzipping ${TAR}"
  gzip -9 "${TAR}"
  # Update checksum for the gzipped file
  ( cd "${OUTDIR}" && sha256sum "${BASENAME}.tar.gz" > "${BASENAME}.tar.gz.sha256" )
fi

# Optional load-test (sanity check the tarball can be imported)
if [[ "$LOAD_TEST" == "1" ]]; then
  echo ">> Load test: importing saved image"
  if [[ "$GZIP" == "1" ]]; then
    docker load -i "${OUTDIR}/${BASENAME}.tar.gz"
  else
    docker load -i "${TAR}"
  fi
fi

echo ">> Done"
if [[ "$GZIP" == "1" ]]; then
  echo "Artifacts:"
  echo "  ${OUTDIR}/${BASENAME}.tar.gz"
  echo "  ${OUTDIR}/${BASENAME}.tar.gz.sha256"
else
  echo "Artifacts:"
  echo "  ${OUTDIR}/${BASENAME}.tar"
  echo "  ${OUTDIR}/${BASENAME}.tar.sha256"
fi

# -------- Bonus: commit-and-save helper (commented) ---
# If you later run a container, mutate it (e.g., run /usr/local/bin/rebuild),
# and want those runtime changes baked into a NEW image tar:
#
# CONTAINER_ID=$(docker run -d --rm "${IMAGE_NAME}:${TAG}" bash -lc "rebuild && sleep 1")
# docker wait "$CONTAINER_ID" || true
# RUNTIME_TAG="${TAG}-runtime"
# docker commit "$CONTAINER_ID" "${IMAGE_NAME}:${RUNTIME_TAG}"
# docker save -o "${OUTDIR}/${IMAGE_NAME//\//_}-${RUNTIME_TAG}.tar" "${IMAGE_NAME}:${RUNTIME_TAG}"
# sha256sum "${OUTDIR}/${IMAGE_NAME//\//_}-${RUNTIME_TAG}.tar" > "${OUTDIR}/${IMAGE_NAME//\//_}-${RUNTIME_TAG}.tar.sha256"


#chmod +x build-and-save.sh
#
## vanilla: build from ./Dockerfile, save tar + checksum
#./build-and-save.sh -n gothoom-build-env -t offline-$(date +%Y%m%d)
#
## gzip to shrink the tarball (~30–70% smaller depending on layers)
#./build-and-save.sh -z
#
## specify a different Dockerfile and context dir, custom outdir
#./build-and-save.sh -f docker/Dockerfile -c docker/ -o ./release-images -t v1.2.3
#
## sanity-load the tar after saving
#./build-and-save.sh -l