# base image
FROM golang:1.17-alpine3.14
ENV CGO_ENABLED 1

RUN apk update && apk add bash inotify-tools git
RUN apk add --update gcc musl-dev

RUN mkdir -p /usr/src/app/backend_go
RUN mkdir /usr/src/app/db
WORKDIR /usr/src/app/backend_go
RUN go install github.com/go-delve/delve/cmd/dlv@latest
RUN sh -c "$(wget -O- https://github.com/deluan/zsh-in-docker/releases/download/v1.1.1/zsh-in-docker.sh)"
COPY ./backend_go ./
RUN go mod vendor
RUN go build -gcflags "all=-N -l" -o /server main.go 

ENTRYPOINT sh start_script.sh

