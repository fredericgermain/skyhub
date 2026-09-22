# Build from the repository root: docker build -f deploy/Dockerfile -t skyhub-mqtt .
FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/skyhub-mqtt ./cmd/skyhub-mqtt

FROM gcr.io/distroless/static:nonroot
COPY --from=build /out/skyhub-mqtt /skyhub-mqtt
USER nonroot
ENTRYPOINT ["/skyhub-mqtt"]
