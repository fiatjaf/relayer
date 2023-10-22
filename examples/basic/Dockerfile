FROM golang:1.21.1

WORKDIR /go/src/app
COPY ./ .

RUN go get -d -v ./...
RUN go install -v ./...
RUN cd examples/basic && make
