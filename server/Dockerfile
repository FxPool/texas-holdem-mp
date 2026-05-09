# Multi-stage Go build. Produces a ~12 MB scratch-based image.
FROM golang:1.21-alpine AS build
WORKDIR /src
# Pre-cache module deps before copying full source for faster rebuilds.
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
# Static build: CGO disabled, target linux/amd64.
ENV CGO_ENABLED=0 GOOS=linux
RUN go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/server /app/server
EXPOSE 18080
USER nonroot:nonroot
ENTRYPOINT ["/app/server"]
