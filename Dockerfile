FROM golang:1.27.2-alpine AS build
WORKDIR /src
ENV CGO_ENABLED=0 GOFLAGS=-trimpath GOMAXPROCS=2
COPY go.mod go.sum ./
COPY standards/go.mod standards/
RUN go mod download
COPY . .
ARG VERSION=dev
RUN go build -p 1 -ldflags "-s -w -X github.com/flaviotinococoutinho/intenza/internal/platform/buildinfo.Version=${VERSION}" -o /out/ ./cmd/...

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/ /usr/local/bin/
USER nonroot:nonroot
EXPOSE 8080 9090
ENTRYPOINT ["/usr/local/bin/intenza-api"]
