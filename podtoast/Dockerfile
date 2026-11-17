# Build from the FarshMac repository root. Target containers need no tools.
FROM --platform=$BUILDPLATFORM golang:1.25 AS build
ARG TARGETOS=linux
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
COPY LICENSE /licenses/MIT.txt
RUN go mod download golang.org/x/net golang.org/x/text \
 && cp /usr/local/go/LICENSE /licenses/Go.txt \
 && cp /go/pkg/mod/golang.org/x/net@*/LICENSE /licenses/x-net.txt \
 && cp /go/pkg/mod/golang.org/x/text@*/LICENSE /licenses/x-text.txt
COPY internal/podtoast ./internal/podtoast
COPY cmd/podtoast ./cmd/podtoast
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags='-s -w' -o /podtoast ./cmd/podtoast
FROM alpine:3.22@sha256:49340b346798875b5592e5d420241f2f9cfba5f47e62ebb1446a1b34734e898f
RUN apk add --no-cache iptables bash coreutils findutils grep sed gawk tar gzip bzip2 xz zip unzip curl wget bind-tools iproute2 iputils netcat-openbsd procps util-linux file less jq openssl ca-certificates \
 && mkdir -p /licenses \
 && apk info -v > /licenses/alpine-packages.txt \
 && cp /lib/apk/db/installed /licenses/apk-installed.txt
LABEL org.opencontainers.image.title="PodToast" \
      org.opencontainers.image.description="Kubernetes ephemeral-container file agent and diagnostic toolbox for FarshMac" \
      org.opencontainers.image.licenses="MIT"
COPY --from=build /podtoast /podtoast
COPY --from=build /licenses /licenses
COPY podtoast/TOOLBOX.md /licenses/toolbox-notices.md
ENTRYPOINT ["/podtoast"]
CMD ["idle"]
