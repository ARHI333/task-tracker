# Этап 1: Сборка приложения
FROM golang:1.26.2-alpine AS builder 

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o server .

# Этап 2: Запуск приложения
FROM alpine:3.19

RUN apk --no-cache add ca-certificates

WORKDIR /app

COPY --from=builder /app/server .

EXPOSE 8080

CMD ["./server"]