FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags '-w -s' main.go

FROM scratch
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

WORKDIR /app


ENV USER_UID=1001
ENV GROUP_UID=1001

COPY --from=builder --chown=${USER_UID}:${GROUP_UID} /app/main .

USER ${USER_UID}:${GROUP_UID}

ENTRYPOINT ["./main"]
CMD ["gql-gateway"]