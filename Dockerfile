FROM golang:1.22

WORKDIR /app

COPY . .

RUN go mod tidy

CMD ["go", "run", "main.go"]