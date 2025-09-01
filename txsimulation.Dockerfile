FROM golang:1.24-alpine3.21

RUN apk add --no-cache make gcc musl-dev linux-headers git

COPY go.mod go.sum /app/
WORKDIR /app
RUN go mod download

COPY . /app
RUN go build -o tx-simulation ./cmd/txsimulation
ENTRYPOINT ["/app/tx-simulation"]
