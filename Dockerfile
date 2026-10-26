
FROM golang:1.26.4 AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o /comment-service ./cmd/main.go

FROM alpine:3.22

WORKDIR /app

COPY --from=builder /comment-service /app/comment-service

EXPOSE 8080

CMD ["/app/comment-service"]
