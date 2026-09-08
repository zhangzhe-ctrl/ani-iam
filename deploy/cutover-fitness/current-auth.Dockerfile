FROM docker.io/library/golang@sha256:1e0126852075c9c60731c8ba49088448b91f63e2aed97ca9d1a9791622a05946 AS build

WORKDIR /src
COPY runtimeadmin ./runtimeadmin
COPY pkg ./pkg
COPY services/auth-service ./services/auth-service

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go work init ./runtimeadmin ./pkg ./services/auth-service && \
    cd services/auth-service && \
    CGO_ENABLED=0 go build -trimpath -tags stdjson -ldflags "-s -w" -o /out/auth-service .

FROM docker.io/library/alpine@sha256:d9e853e87e55526f6b2917df91a2115c36dd7c696a35be12163d44e6e2a4b6bc

RUN apk add --no-cache ca-certificates && \
    adduser -D -H -u 65532 ani

COPY --from=build /out/auth-service /usr/local/bin/auth-service

USER 65532:65532
EXPOSE 9101 9201
ENTRYPOINT ["/usr/local/bin/auth-service"]
