# syntax=docker/dockerfile:1

# Shared build stage: compiled once and reused by both final images.
FROM golang:1.27-alpine AS build
WORKDIR /src

# go.sum is optional so the image builds before any dependency is added.
COPY go.mod go.su[m] ./
RUN go mod download

COPY cmd/ cmd/
COPY internal/ internal/

RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/detector   ./cmd/detector
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/api-server ./cmd/api-server

FROM gcr.io/distroless/static-debian12:nonroot AS detector
COPY --from=build /out/detector /detector
USER nonroot:nonroot
ENTRYPOINT ["/detector"]

FROM gcr.io/distroless/static-debian12:nonroot AS api-server
COPY --from=build /out/api-server /api-server
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/api-server"]
