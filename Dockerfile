FROM golang:latest AS build_base

WORKDIR /tmp/app

COPY go.mod .
COPY go.sum .

RUN go mod download

COPY . .

RUN go build -o /tmp/app/main.bin ./main.go
RUN go build -o /tmp/app/console.bin ./console/console.go

# Start fresh from a smaller image
FROM debian

WORKDIR /app

RUN apt update && apt upgrade -y
RUN apt install ffmpeg -y
COPY --from=build_base /tmp/app/main.bin ./
COPY --from=build_base /tmp/app/console.bin ./
COPY --from=build_base /tmp/app/views ./views/

RUN ./console.bin database:fresh seed:adminuser

# RUN cd /app && ./console database:fresh seed:adminuser
EXPOSE 3000

CMD ["./main.bin"]