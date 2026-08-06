FROM golang:1.24-alpine AS builder

WORKDIR /app

# Copy the entire relayer project (since we're building from the root context)
COPY . .

# Change to the nip5-access directory
WORKDIR /app/examples/nip5-access

# Download dependencies
RUN go mod download

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o nip5-access-relay .

# Final stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /root/

# Copy the binary from builder stage
COPY --from=builder /app/examples/nip5-access/nip5-access-relay .

# Expose port
EXPOSE 7447

# Run the binary
CMD ["./nip5-access-relay"]



