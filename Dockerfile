FROM golang:1.22-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /dbx-server ./cmd/dbx-server

FROM alpine:3.19 AS certs
RUN apk add --no-cache ca-certificates

FROM scratch
COPY --from=certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /dbx-server /usr/local/bin/dbx-server
USER 65534:65534
ENTRYPOINT ["dbx-server"]
CMD ["serve"]
