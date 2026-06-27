FROM golang:1.26-alpine AS build
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w -extldflags=-static" \
    -trimpath \
    -o /app/server ./cmd/api

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /app/server /server
EXPOSE 8080
ENTRYPOINT ["/server"]
