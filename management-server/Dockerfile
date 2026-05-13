# syntax=docker/dockerfile:1
FROM golang:1.22-bookworm AS build
WORKDIR /src
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/management-server ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/management-server /management-server
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/management-server"]
