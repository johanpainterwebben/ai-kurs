FROM golang:1.26-alpine AS build
WORKDIR /src
# Moduler först, så att de cachas så länge go.mod/go.sum inte ändras.
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
# Utan -tags mock: de sparade svaren och TNU_MOCK ingår inte i produktionsbinären.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/temperatur .

# distroless/static innehåller CA-certifikat (för HTTPS mot API:et) men inget skal.
FROM gcr.io/distroless/static:nonroot
COPY --from=build /out/temperatur /temperatur
EXPOSE 8080
USER nonroot:nonroot
# Imagen saknar curl och skal, så kontrollen är programmet självt.
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 CMD ["/temperatur", "healthcheck"]
ENTRYPOINT ["/temperatur"]
