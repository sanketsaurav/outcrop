FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /outcrop ./cmd/outcrop
RUN mkdir /data-skel

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /outcrop /outcrop
# Ship /data owned by nonroot (uid 65532) so the anonymous volume is writable.
COPY --from=build --chown=65532:65532 /data-skel /data
ENV DATA_DIR=/data
VOLUME /data
EXPOSE 8080
USER nonroot
ENTRYPOINT ["/outcrop"]
