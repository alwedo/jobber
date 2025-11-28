FROM golang:latest AS build
WORKDIR /build
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o app .
FROM scratch
COPY --from=build /build/app /app
EXPOSE 80
ENTRYPOINT ["/app"]
