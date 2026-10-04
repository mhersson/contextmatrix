# Stage 1: Build frontend
FROM node:26-alpine@sha256:0b36e8c136b94cd4fcf02188228e76c31ad5872eef3fec8cbd2eee500cfd9e80 AS frontend
WORKDIR /build/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# Stage 2: Build Go binary with embedded frontend
FROM golang:1.27.1-alpine@sha256:8a5910f31396cd4d89662f56c68b3ae31d374308270a1c3bd96672ee5ed43414 AS backend
RUN apk add --no-cache git
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /build/web/dist ./web/dist
ARG VERSION=""
ARG GIT_COMMIT=""
ARG BUILD_TIME=""
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath \
      -ldflags="-s -w -X main.version=${VERSION} -X main.gitCommit=${GIT_COMMIT} -X 'main.buildTime=${BUILD_TIME}'" \
      -o /contextmatrix ./cmd/contextmatrix

# Stage 3: Minimal runtime
FROM alpine:3.24@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6
RUN apk --no-cache upgrade \
    && apk --no-cache add git openssh-client ca-certificates
COPY --from=backend /contextmatrix /usr/local/bin/contextmatrix
COPY workflow-skills/ /etc/contextmatrix/skills/
ENV CONTEXTMATRIX_WORKFLOW_SKILLS_DIR=/etc/contextmatrix/skills
ENV HOME=/home/nobody
RUN mkdir -p /home/nobody && chown nobody:nobody /home/nobody
USER 65534:65534
ENTRYPOINT ["contextmatrix"]
