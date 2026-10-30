# syntax=docker/dockerfile:1
FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/adserver ./cmd/adserver

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/adserver /adserver
EXPOSE 8080
USER nonroot:nonroot
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 CMD ["/adserver", "healthcheck"]
ENTRYPOINT ["/adserver"]
