# backend 多阶段构建
FROM golang:1.22 AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/backend .

FROM gcr.io/distroless/static-debian12
WORKDIR /app
COPY --from=build /out/backend /app/backend
EXPOSE 8080
ENTRYPOINT ["/app/backend"]
