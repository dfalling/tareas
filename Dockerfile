FROM golang:1.27-alpine AS build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /app ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /app /app
EXPOSE 8080
ENTRYPOINT ["/app"]
