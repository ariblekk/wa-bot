FROM golang:1.25-bookworm AS build

WORKDIR /src

# A dependency requires Go >= 1.25, so the image must not be older than that.
ENV GOPROXY=https://proxy.golang.org,direct
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/wa-chatbot ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/wa-chatbot /wa-chatbot
COPY --from=build /src/prompts /app/prompts
EXPOSE 8080
ENTRYPOINT ["/wa-chatbot"]
