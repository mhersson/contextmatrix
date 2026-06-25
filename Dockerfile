# Stage 1: Build frontend
FROM node:26-alpine@sha256:3ad34ca6292aec4a91d8ddeb9229e29d9c2f689efd0dd242860889ac71842eba AS frontend
WORKDIR /build/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# Stage 2: Build Go binary with embedded frontend
FROM golang:1.26-alpine@sha256:3ad57304ad93bbec8548a0437ad9e06a455660655d9af011d58b993f6f615648 AS backend
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
FROM alpine:3.24@sha256:a2d49ea686c2adfe3c992e47dc3b5e7fa6e6b5055609400dc2acaeb241c829f4
RUN apk --no-cache upgrade \
    && apk --no-cache add git openssh-client ca-certificates
COPY --from=backend /contextmatrix /usr/local/bin/contextmatrix
COPY workflow-skills/ /etc/contextmatrix/skills/
ENV CONTEXTMATRIX_WORKFLOW_SKILLS_DIR=/etc/contextmatrix/skills
ENV HOME=/home/nobody
RUN mkdir -p /home/nobody && chown nobody:nobody /home/nobody
USER nobody
ENTRYPOINT ["contextmatrix"]
