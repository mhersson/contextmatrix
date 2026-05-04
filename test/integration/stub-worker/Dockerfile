FROM golang:1.26-bookworm AS build
WORKDIR /src
COPY go.mod ./
COPY *.go ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/claude .

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates git && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/claude /usr/local/bin/claude
COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh
RUN useradd -m -u 1000 user
USER user
WORKDIR /home/user
ENTRYPOINT ["/entrypoint.sh"]
