FROM golang:1.26.1-alpine AS build

# The build context is the repository root: this module replaces the contracts, lifecycle and platform modules with paths
# (`replace … => ../contracts`), so all four trees have to be present for `go mod download` to read the
# requirements and for the compile to find the generated code.
WORKDIR /src
COPY backend/contracts/go.mod backend/contracts/go.sum ./backend/contracts/
COPY backend/lifecycle/go.mod ./backend/lifecycle/
COPY backend/platform/go.mod backend/platform/go.sum ./backend/platform/
COPY backend/auth/go.mod backend/auth/go.sum ./backend/auth/
RUN cd backend/auth && go mod download

COPY backend/contracts/ ./backend/contracts/
COPY backend/lifecycle/ ./backend/lifecycle/
COPY backend/platform/ ./backend/platform/
COPY backend/auth/ ./backend/auth/
RUN cd backend/auth && CGO_ENABLED=0 go build -trimpath -o /auth ./cmd/auth

FROM scratch
USER 65532:65532
COPY --from=build /auth /auth
EXPOSE 9093 9094
ENTRYPOINT ["/auth"]
