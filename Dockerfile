FROM golang:1.24 AS builder

WORKDIR /app

COPY go.mod .
COPY go.sum .
RUN go mod download

COPY . .

RUN GOOS=linux GOARCH=amd64 go build -o /go/bin/app main.go

FROM alpine:latest

WORKDIR /app
RUN apk add --no-cache libc6-compat file

COPY --from=builder /go/bin/app /bin/app

EXPOSE 8080
EXPOSE 9090

CMD ["/bin/app"]