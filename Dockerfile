# ---- build stage ----
FROM golang:1.23-alpine AS build
WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /journalflow ./cmd/server

# ---- run stage ----
FROM alpine:3.20
WORKDIR /app

RUN apk add --no-cache ca-certificates tzdata

COPY --from=build /journalflow ./journalflow
COPY web ./web
COPY migrations ./migrations

EXPOSE 8080
ENTRYPOINT ["./journalflow"]
