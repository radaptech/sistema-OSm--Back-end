FROM golang:1.26.5-alpine

WORKDIR /app

# Instala git e as ferramentas essenciais de compilação
RUN apk add --no-cache git build-base

# Instala o CompileDaemon
RUN go install github.com/githubnemo/CompileDaemon@latest

# Baixa as dependências
COPY go.mod go.sum ./
RUN go mod download

# Adicionado -buildvcs=false para ignorar a verificação do Git
ENTRYPOINT ["CompileDaemon", "-log-prefix=false", "-build=go build -buildvcs=false -o main .", "-command=./main"]