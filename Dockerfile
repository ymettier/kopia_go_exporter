FROM golang:1.25 AS builder

WORKDIR /go

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -v -o kopia-go-exporter .

FROM gcr.io/distroless/static-debian12

COPY --from=builder /go/kopia-go-exporter /kopia-go-exporter

EXPOSE 9090

ENTRYPOINT ["/kopia-go-exporter"]

