FROM golang:1.22-alpine AS build

WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/wa-chatbot ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/wa-chatbot /wa-chatbot
COPY --from=build /src/prompts /app/prompts
EXPOSE 8080
ENTRYPOINT ["/wa-chatbot"]
