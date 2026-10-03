FROM golang:1.25 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/tollgate ./cmd/tollgate \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/fakeupstream ./cmd/fakeupstream

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/tollgate /tollgate
COPY --from=build /out/fakeupstream /fakeupstream
COPY config.example.yaml /etc/tollgate/tollgate.yaml
EXPOSE 8787
USER nonroot:nonroot
ENTRYPOINT ["/tollgate", "-config", "/etc/tollgate/tollgate.yaml"]
