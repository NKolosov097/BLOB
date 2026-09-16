FROM golang:1.27.1-alpine AS go-base
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

FROM go-base AS build
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api \
    && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/worker ./cmd/worker

FROM gcr.io/distroless/static-debian12:nonroot AS application
ARG TARGET=api
COPY --from=build /out/${TARGET} /app
USER nonroot:nonroot
CMD ["/app"]

FROM go-base AS migration-tools
RUN addgroup -S migrator && adduser -S -D -G migrator migrator
ENV GOCACHE=/tmp/go-build
COPY --chown=migrator:migrator migrations/ ./migrations/
USER migrator:migrator
ENTRYPOINT ["go", "tool", "goose", "-dir", "/src/migrations"]
