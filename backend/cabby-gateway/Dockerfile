FROM golang:1.26.1-alpine AS build

# The build context is the repository root: this module replaces the contracts and lifecycle modules with paths
# (`replace … => ../contracts`), so all three trees have to be present for `go mod download` to read the
# requirements and for the compile to find the generated code.
WORKDIR /src
COPY backend/contracts/go.mod backend/contracts/go.sum ./backend/contracts/
COPY backend/lifecycle/go.mod ./backend/lifecycle/
COPY backend/cabby-gateway/go.mod backend/cabby-gateway/go.sum ./backend/cabby-gateway/
RUN cd backend/cabby-gateway && go mod download

COPY backend/contracts/ ./backend/contracts/
COPY backend/lifecycle/ ./backend/lifecycle/
COPY backend/cabby-gateway/ ./backend/cabby-gateway/
RUN cd backend/cabby-gateway && CGO_ENABLED=0 go build -trimpath -o /cabby-gateway ./cmd/cabby-gateway

FROM scratch
USER 65532:65532
COPY --from=build /cabby-gateway /cabby-gateway
EXPOSE 8080 9091
ENTRYPOINT ["/cabby-gateway"]
