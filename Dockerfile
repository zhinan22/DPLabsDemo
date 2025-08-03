FROM golang:1.24-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /usr/local/bin/app ./main.go

FROM alpine:latest
RUN apk --no-cache add ca-certificates
COPY --from=builder /usr/local/bin/app /app
COPY .env /.env
EXPOSE 8080
CMD ["/app"]