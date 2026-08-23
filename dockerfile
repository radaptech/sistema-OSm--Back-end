FROM golang:1.26.5-alpine

WORKDIR /app

# Instala git, ferramentas essenciais de compilação e o pg_dump (versão 16,
# pareada com o Postgres do projeto -- pg_dump precisa ser da mesma major
# version do servidor, ou mais nova) para o subcomando backup-banco.
RUN apk add --no-cache git build-base postgresql16-client

# Instala o CompileDaemon
RUN go install github.com/githubnemo/CompileDaemon@latest

# Baixa as dependências
COPY go.mod go.sum ./
RUN go mod download

# Adicionado -buildvcs=false para ignorar a verificação do Git
ENTRYPOINT ["CompileDaemon", "-log-prefix=false", "-build=go build -buildvcs=false -o main .", "-command=./main"]