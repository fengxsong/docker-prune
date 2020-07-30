FROM golang:1.14-alpine as build
ENV GOOS=linux CGO_ENABLED=0 GOPROXY=https://goproxy.cn
WORKDIR /src
COPY go.mod /src/
COPY main.go /src/
RUN go build -o docker-prune main.go

FROM alpine:3.10
COPY --from=build /src/docker-prune /usr/local/bin/docker-prune
ENTRYPOINT ["/usr/local/bin/docker-prune"]
