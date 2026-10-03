FROM golang:1.26.1-alpine AS build

# The build context is the repository root: this module replaces the lifecycle module with a path
# (`replace … => ../lifecycle`), so both trees have to be present for `go mod download` to read the
# requirements and for the compile to find the shared code. The web page is embedded from
# internal/ui, so it travels with the module sources.
WORKDIR /src
COPY backend/lifecycle/go.mod ./backend/lifecycle/
COPY backend/dbviewer/go.mod backend/dbviewer/go.sum ./backend/dbviewer/
RUN cd backend/dbviewer && go mod download

COPY backend/lifecycle/ ./backend/lifecycle/
COPY backend/dbviewer/ ./backend/dbviewer/
RUN cd backend/dbviewer && CGO_ENABLED=0 go build -trimpath -o /dbviewer ./cmd/dbviewer

FROM scratch
USER 65532:65532
COPY --from=build /dbviewer /dbviewer
EXPOSE 8090
ENTRYPOINT ["/dbviewer"]
