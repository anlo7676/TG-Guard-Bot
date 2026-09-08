FROM alpine:3.23 AS download
ARG TARGETARCH
ARG RELEASE_VERSION=v1.10.0
RUN apk add --no-cache ca-certificates \
    && case "$TARGETARCH" in amd64|arm64) ;; *) echo "Unsupported architecture: $TARGETARCH"; exit 1;; esac \
    && mkdir /out && cd /out \
    && base="https://github.com/anlo7676/TG-Guard-Bot/releases/download/${RELEASE_VERSION}" \
    && wget -q "$base/tgguard-linux-$TARGETARCH" \
    && wget -q "$base/SHA256SUMS" \
    && grep "  tgguard-linux-$TARGETARCH\$" SHA256SUMS > selected.sha256 \
    && test -s selected.sha256 && sha256sum -c selected.sha256 \
    && mv "tgguard-linux-$TARGETARCH" tgguard && chmod 0755 tgguard

FROM alpine:3.23
RUN apk add --no-cache ca-certificates tzdata && addgroup -S -g 10001 app && adduser -S -u 10001 -G app app
COPY --from=download /out/tgguard /usr/local/bin/tgguard
USER app
EXPOSE 8080
ENTRYPOINT ["tgguard"]
