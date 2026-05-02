# Stub fake-claude worker for the contextmatrix integration harness.
# Mirrors cm-orchestrated:test's runtime shape — long-lived container
# the runner spawns claude into via Docker exec — but with a fake
# claude binary so phase actions return canned markers in milliseconds.
# See README.md in this directory for the protocol.

FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY *.go ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /stub-claude .

FROM alpine:3.23
RUN apk add --no-cache bash git tini ca-certificates
COPY --from=build /stub-claude /usr/local/bin/claude
COPY entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh \
    && adduser -D -u 1000 user \
    && mkdir -p /workspace \
    && chown user:user /workspace
USER user
WORKDIR /home/user

ENV CM_WORKER_MODE=stub-orchestrated

# tini reaps zombies so claude subprocess output flushes before exit.
ENTRYPOINT ["/sbin/tini", "--"]
CMD ["/usr/local/bin/entrypoint.sh"]
