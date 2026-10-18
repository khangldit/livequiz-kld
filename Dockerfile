# ---- Stage 1: build ----
# Đổi 1.27.2 cho khớp với số version trong `go version` / dòng `go` của go.mod
FROM golang:1.27.2-alpine AS build
WORKDIR /src

# Copy go.mod/go.sum trước để Docker cache bước tải dependency
COPY go.* ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /app ./cmd/api

# ---- Stage 2: run ----
FROM gcr.io/distroless/static-debian12
COPY --from=build /app /app
EXPOSE 8080
ENTRYPOINT ["/app"]

