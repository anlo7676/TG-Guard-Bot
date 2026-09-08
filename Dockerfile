FROM alpine:3.23 AS runtime-base
RUN (timeout 180 apk add --no-cache ca-certificates tzdata \
     || { echo "Alpine package installation failed or exceeded 180 seconds; check container DNS and Alpine repository connectivity." >&2; exit 1; }) \
    && addgroup -S -g 10001 app && adduser -S -u 10001 -G app app

FROM runtime-base AS download
ARG TARGETARCH
ARG RELEASE_VERSION=v1.11.1
RUN case "$TARGETARCH" in amd64|arm64) ;; *) echo "Unsupported architecture: $TARGETARCH"; exit 1;; esac \
    && mkdir /out && cd /out \
    && base="https://github.com/anlo7676/TG-Guard-Bot/releases/download/${RELEASE_VERSION}" \
    && echo "Downloading precompiled TG Guard ${RELEASE_VERSION} for ${TARGETARCH}" \
    && (timeout 240 wget -T 30 "$base/tgguard-linux-$TARGETARCH" \
        && timeout 60 wget -T 30 "$base/SHA256SUMS" \
        || { echo "Release download failed or timed out; check GitHub release asset connectivity." >&2; exit 1; }) \
    && grep "  tgguard-linux-$TARGETARCH\$" SHA256SUMS > selected.sha256 \
    && test -s selected.sha256 && sha256sum -c selected.sha256 \
    && mv "tgguard-linux-$TARGETARCH" tgguard && chmod 0755 tgguard

FROM runtime-base
COPY --from=download /out/tgguard /usr/local/bin/tgguard
USER app
EXPOSE 8080
ENTRYPOINT ["tgguard"]
