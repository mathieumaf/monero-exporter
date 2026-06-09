ARG BUILDER_IMAGE=golang:1.26-alpine
ARG RUNTIME_IMAGE=alpine:latest


FROM $BUILDER_IMAGE AS builder

        WORKDIR /workspace

        # Resolve dependencies first: this layer is cached and only re-runs when
        # go.mod / go.sum change, not on every source edit.
        COPY go.mod go.sum ./
        RUN go mod download

        COPY pkg/ pkg/
        COPY cmd/ cmd/

        # TARGETOS / TARGETARCH are injected by BuildKit, so the same Dockerfile
        # cross-compiles for whatever --platform is requested (amd64, arm64, ...).
        # VERSION / COMMIT are stamped into the binary; the release workflow passes
        # the tag and sha, local builds fall back to "dev".
        ARG TARGETOS TARGETARCH
        ARG VERSION=dev
        ARG COMMIT=dev
        RUN set -x && \
                CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
                go build -v \
                        -trimpath \
                        -buildvcs=false \
                        -tags osusergo,netgo,static_build \
                        -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" \
                        -o monero-exporter \
                                ./cmd/monero-exporter


FROM $RUNTIME_IMAGE

        # ca-certificates lets the exporter reach an RPC endpoint over HTTPS; the
        # rest of alpine (shell, apk) stays available for debugging the container.
        RUN apk add --no-cache ca-certificates && \
                adduser -D -H -u 65532 nonroot

        COPY --from=builder /workspace/monero-exporter /monero-exporter

        USER nonroot
        EXPOSE 9000

        ENTRYPOINT ["/monero-exporter"]
