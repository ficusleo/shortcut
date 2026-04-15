FROM golang:1.24 AS builder

WORKDIR /app

COPY go.mod .
COPY go.sum .
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o /go/bin/app main.go

FROM alpine:latest

WORKDIR /app
RUN apk add --no-cache libc6-compat file

COPY --from=builder /go/bin/app /bin/app
COPY config.yml /app/config.yml

EXPOSE 8080
EXPOSE 9090

CMD ["/bin/app"]