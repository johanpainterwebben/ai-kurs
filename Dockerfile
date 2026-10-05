FROM golang:1.26-alpine AS build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/temperatur .

# distroless/static innehåller CA-certifikat (för HTTPS mot API:et) men inget skal.
FROM gcr.io/distroless/static:nonroot
COPY --from=build /out/temperatur /temperatur
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/temperatur"]
