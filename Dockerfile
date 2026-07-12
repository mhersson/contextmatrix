# Stage 1: Build frontend
FROM node:26-alpine@sha256:e88a35be04478413b7c71c455cd9865de9b9360e1f43456be5951032d7ac1a66 AS frontend
WORKDIR /build/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# Stage 2: Build Go binary with embedded frontend
FROM golang:1.26-alpine@sha256:0178a641fbb4858c5f1b48e34bdaabe0350a330a1b1149aabd498d0699ff5fb2 AS backend
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
FROM alpine:3.24@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b
RUN apk --no-cache upgrade \
    && apk --no-cache add git openssh-client ca-certificates
COPY --from=backend /contextmatrix /usr/local/bin/contextmatrix
COPY workflow-skills/ /etc/contextmatrix/skills/
ENV CONTEXTMATRIX_WORKFLOW_SKILLS_DIR=/etc/contextmatrix/skills
ENV HOME=/home/nobody
RUN mkdir -p /home/nobody && chown nobody:nobody /home/nobody
USER nobody
ENTRYPOINT ["contextmatrix"]
