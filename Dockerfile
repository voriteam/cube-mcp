FROM golang:1.23-alpine AS build
WORKDIR /src

COPY go.mod go.sum* ./
RUN go mod download

COPY *.go ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/cube-mcp .

FROM gcr.io/distroless/static-debian12:nonroot
ARG COMMIT_SHA=""
ENV COMMIT_SHA=${COMMIT_SHA}
COPY --from=build /out/cube-mcp /cube-mcp
EXPOSE 8003
USER nonroot:nonroot
ENTRYPOINT ["/cube-mcp"]
