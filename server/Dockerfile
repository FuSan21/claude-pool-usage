FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG TARGETOS TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /pool-usage .

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /pool-usage /pool-usage
EXPOSE 8090
USER nonroot:nonroot
ENTRYPOINT ["/pool-usage"]
