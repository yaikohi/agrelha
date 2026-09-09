# --- Build stage: mise provides go, bun, templ ---
FROM alpine:3.22.2 AS build

RUN apk add --no-cache curl unzip git libstdc++ libgcc
RUN curl -fsSL https://mise.jdx.dev/install.sh | sh
ENV PATH="/root/.local/bin:$PATH"
# Pure-Go build (modernc sqlite, no cgo) -> fully static binary.
ENV CGO_ENABLED=0

WORKDIR /app

# Cache tooling
COPY mise.toml Taskfile.yml ./
RUN mise trust && mise exec task -- task setup

# Cache JS + Go deps
COPY package.json ./
RUN mise exec -- task tailwind:deps
COPY go.mod go.sum* ./
RUN mise exec -- go mod download

# Build (templ generate + tailwind + datastar vendor + go build)
ARG VERSION=dev
COPY . .
RUN mise exec -- task build VERSION=${VERSION}

# --- Prod stage ---
FROM alpine:3.20 AS prod
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=build /app/main /app/main
EXPOSE 8080
CMD ["./main"]
