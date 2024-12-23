FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o main .

FROM alpine:3.18
WORKDIR /app
COPY --from=builder /app/main .
COPY definitions ./definitions
ENTRYPOINT ["./main"]
CMD ["start", "--watched-dir=./definitions"]