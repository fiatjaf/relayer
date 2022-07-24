FROM golang:1.18

WORKDIR /go/src/app
COPY ./ .

RUN go get -d -v ./...
RUN go install -v ./...
RUN cd basic && make
