FROM golang:1.24-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /civcat .

FROM alpine:3.21

RUN apk add --no-cache ca-certificates
COPY --from=build /civcat /usr/local/bin/civcat

# Default ComfyUI mount point.
VOLUME ["/comfyui"]
VOLUME ["/root/.civcat"]

ENTRYPOINT ["civcat"]
