# SIGNL Download-Server — Docker
#   docker build -t signl-server .
#   docker run -d --name signl-server -p 8484:8484 -v signl-data:/data signl-server
FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY *.go ./
COPY web ./web
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.Version=${VERSION}" -o /signl-server .

FROM alpine:3.20
# ffmpeg: für Streams (HLS), die nicht als einzelne Datei vorliegen.
RUN apk add --no-cache ffmpeg ca-certificates tzdata
COPY --from=build /signl-server /usr/local/bin/signl-server
VOLUME /data
EXPOSE 8484
ENV SIGNL_DATA=/data
ENTRYPOINT ["signl-server"]
