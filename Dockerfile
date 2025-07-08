FROM golang:1.23

# Install FFmpeg
RUN apt-get update && apt-get install -y ffmpeg

WORKDIR /app

# Copy the Go mod and sum files first for caching dependencies
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy the rest of the application code
COPY . .

# Build the application
RUN go build -o my-app .

# Expose the port your app runs on
EXPOSE 8086

# Run the application
CMD ["./my-app"]
