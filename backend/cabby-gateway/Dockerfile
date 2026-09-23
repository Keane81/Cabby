FROM golang:1.26.1-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /cabby-gateway ./cmd/cabby-gateway

FROM scratch
USER 65532:65532
COPY --from=build /cabby-gateway /cabby-gateway
EXPOSE 8080 9091
ENTRYPOINT ["/cabby-gateway"]
