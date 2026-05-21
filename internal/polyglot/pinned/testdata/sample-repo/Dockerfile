FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o app .

FROM alpine:3.20
COPY --from=builder /app/app /usr/local/bin/app
