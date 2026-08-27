FROM golang:1.22-alpine AS build
WORKDIR /src
RUN apk add --no-cache gcc musl-dev sqlite-dev
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 go build -o /server ./cmd/server

FROM alpine:3.20
RUN apk add --no-cache ca-certificates sqlite-libs
WORKDIR /app
COPY --from=build /server /app/server
EXPOSE 8080
ENTRYPOINT ["/app/server"]
