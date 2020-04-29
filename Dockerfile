FROM golang:1.14.2 AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download -x
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build

FROM alpine:3 as trentino
RUN apk add -U --no-cache ca-certificates

FROM scratch
WORKDIR /app
COPY --from=trentino /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /app/pepega .
ENTRYPOINT [ "./pepega" ]