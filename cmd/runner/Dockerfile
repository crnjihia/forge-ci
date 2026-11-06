# path: cmd/runner/Dockerfile

FROM golang:1.22-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . ./
RUN CGO_ENABLED=0 GOOS=linux go build -o /bin/forge-runner ./cmd/runner

FROM alpine:3.19

RUN apk --no-cache add ca-certificates tzdata docker-cli

WORKDIR /app
COPY --from=builder /bin/forge-runner /app/forge-runner

ENTRYPOINT ["/app/forge-runner"]

