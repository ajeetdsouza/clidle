FROM golang:1.23.4-alpine

WORKDIR /app

COPY go.mod .
COPY go.sum .
RUN go mod download

COPY *.go .
COPY store/*.go store/
COPY schema.sql .
RUN go build -o clidle .

FROM scratch

COPY --from=0 /app/clidle .

ENV CLICOLOR_FORCE=1
ENV CLIDLE_DATA_DIR=/opt/clidle/data
CMD ["./clidle", "-serve", "0.0.0.0:22"]
