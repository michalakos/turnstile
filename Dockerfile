# --- Build stage ---
FROM golang:1.25-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o /turnstile ./cmd/turnstile

# --- Runtime stage ---
FROM alpine

COPY --from=builder /turnstile /turnstile

EXPOSE 50051

CMD ["/turnstile"]
