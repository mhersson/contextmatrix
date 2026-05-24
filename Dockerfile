# Stage 1: Build frontend
FROM node:26-alpine@sha256:7c6af15abe4e3de859690e7db171d0d711bf37d27528eddfe625b2fe89e097f8 AS frontend
WORKDIR /build/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# Stage 2: Build Go binary with embedded frontend
FROM golang:1.26-alpine@sha256:91eda9776261207ea25fd06b5b7fed8d397dd2c0a283e77f2ab6e91bfa71079d AS backend
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
FROM alpine:3.23@sha256:5b10f432ef3da1b8d4c7eb6c487f2f5a8f096bc91145e68878dd4a5019afde11
RUN apk --no-cache upgrade \
    && apk --no-cache add git openssh-client ca-certificates
COPY --from=backend /contextmatrix /usr/local/bin/contextmatrix
COPY workflow-skills/ /etc/contextmatrix/skills/
ENV CONTEXTMATRIX_WORKFLOW_SKILLS_DIR=/etc/contextmatrix/skills
ENV HOME=/home/nobody
RUN mkdir -p /home/nobody && chown nobody:nobody /home/nobody
USER nobody
ENTRYPOINT ["contextmatrix"]
