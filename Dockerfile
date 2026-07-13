FROM golang:1.26 AS builder

ARG VERSION=build

WORKDIR /go

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN echo "${VERSION}" > version.txt \
    && CGO_ENABLED=0 go build -o kopia-go-exporter .

FROM gcr.io/distroless/static-debian13:nonroot

COPY --from=builder /go/kopia-go-exporter /kopia-go-exporter

EXPOSE 9090

ENTRYPOINT ["/kopia-go-exporter"]

