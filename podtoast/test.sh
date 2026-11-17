#!/usr/bin/env bash
# Uses local images by default, or PODTOAST_TEST_IMAGE for an external agent image.
# Creates a uniquely named disposable kind cluster.
# Does not read or change the user's default kubeconfig/current context.
set -euo pipefail
cd "$(dirname "$0")/.."
for tool in go docker kind; do command -v "$tool" >/dev/null; done
work=$(mktemp -d "${TMPDIR:-/tmp}/podtoast-test.XXXXXX")
cluster="podtoast-test-$$"
created=false
cleanup() {
  if $created; then kind delete cluster --name "$cluster"; fi
  rm -rf "$work"
}
trap cleanup EXIT
arch=$(docker info --format '{{.Architecture}}')
case "$arch" in aarch64|arm64) arch=arm64 ;; x86_64|amd64) arch=amd64 ;; *) echo "Unsupported Docker architecture: $arch" >&2; exit 1 ;; esac
CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -o "$work/fixture" ./podtoast/testdata/fixture.go
CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go test -c -o "$work/tests" ./internal/podtoast
cat > "$work/Dockerfile" <<'DOCKER'
FROM scratch
COPY fixture /fixture
ENTRYPOINT ["/fixture"]
DOCKER
cat > "$work/Tests.Dockerfile" <<'DOCKER'
FROM scratch
COPY tests /tests
ENTRYPOINT ["/tests"]
DOCKER
if [ -z "${PODTOAST_TEST_IMAGE:-}" ]; then
  docker build -f podtoast/Dockerfile -t podtoast:dev .
fi
docker build -t podtoast-fixture:dev "$work"
docker build -f "$work/Tests.Dockerfile" -t podtoast-tests:dev "$work"
docker run --rm --network none --tmpfs /tmp podtoast-tests:dev -test.v
created=true
kind create cluster --name "$cluster" --image kindest/node:v1.32.0 --kubeconfig "$work/kubeconfig" --wait 60s
kind load docker-image --name "$cluster" podtoast-fixture:dev
if [ -z "${PODTOAST_TEST_IMAGE:-}" ]; then
  kind load docker-image --name "$cluster" podtoast:dev
fi
PODTOAST_TEST_KUBECONFIG="$work/kubeconfig" go test -race -v ./internal/kube -run '^TestPodToastDistroless$' -count=1 -timeout=240s
