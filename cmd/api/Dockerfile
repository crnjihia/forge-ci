# path: cmd/api/Dockerfile

FROM golang:1.22-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . ./
RUN CGO_ENABLED=0 GOOS=linux go build -o /bin/forge-api ./cmd/api

FROM alpine:3.19

RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app
COPY --from=builder /bin/forge-api /app/forge-api
COPY migrations /app/migrations

EXPOSE 8080

ENTRYPOINT ["/app/forge-api"]

