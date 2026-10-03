FROM golang:1.26.1-alpine AS build

# The build context is the repository root: this module replaces the contracts, lifecycle and platform modules with paths
# (`replace … => ../contracts`), so all four trees have to be present for `go mod download` to read the
# requirements and for the compile to find the generated code.
WORKDIR /src
COPY backend/contracts/go.mod backend/contracts/go.sum ./backend/contracts/
COPY backend/lifecycle/go.mod ./backend/lifecycle/
COPY backend/platform/go.mod backend/platform/go.sum ./backend/platform/
COPY backend/location/go.mod backend/location/go.sum ./backend/location/
RUN cd backend/location && go mod download

COPY backend/contracts/ ./backend/contracts/
COPY backend/lifecycle/ ./backend/lifecycle/
COPY backend/platform/ ./backend/platform/
COPY backend/location/ ./backend/location/
RUN cd backend/location && CGO_ENABLED=0 go build -trimpath -o /location ./cmd/location

FROM scratch
USER 65532:65532
COPY --from=build /location /location
EXPOSE 9095 9096
ENTRYPOINT ["/location"]
