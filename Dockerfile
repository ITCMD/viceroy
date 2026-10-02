# Viceroy container image: one static binary on distroless, data in /data.
#   docker build -t viceroy .
#   docker run -d -p 8420:8420 -v viceroy-data:/data viceroy
# See deploy/compose.yaml and docs/DEPLOY.md.

FROM node:24-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/viceroy ./cmd/viceroy \
 && mkdir -p /out/data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/viceroy /usr/local/bin/viceroy
COPY --from=build --chown=nonroot:nonroot /out/data /data
# Container-specific settings; everything else is in /data/viceroy.toml (written on first start).
ENV VICEROY_LISTEN=0.0.0.0:8420 \
    VICEROY_DATA_DIR=/data
VOLUME /data
EXPOSE 8420
ENTRYPOINT ["/usr/local/bin/viceroy", "-config", "/data/viceroy.toml"]
CMD ["serve", "-init"]
