# Imagem de produção -- multi-stage, sem CompileDaemon. O hot-reload de dev
# fica em dockerfile.dev (usado pelo ../docker-compose.yml); este aqui é o
# que o Railway builda (Settings > Build > Dockerfile Path, se não pegar o
# nome "dockerfile" sozinho).
#
# CGO_ENABLED=0 porque nada no projeto usa cgo (pgx/v5 é Go puro) -- dá um
# binário estático, então o estágio final nem precisa de libc glibc/musl
# compatível, só do próprio binário + pg_dump.
FROM golang:1.26.5-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -buildvcs=false -o main .

# alpine:3.24 pareado com a versão por baixo de golang:1.26.5-alpine -- pg_dump
# aqui é o mesmo caminho do backup-banco, só que rodando em produção de verdade.
FROM alpine:3.24

WORKDIR /app

# ca-certificates: sem isso toda chamada HTTPS pro R2 falha na validação do
# certificado. postgresql17-client: só o pg_dump/pg_restore, não o servidor
# inteiro (postgresql17 puxaria o daemon, que este container nunca roda).
#
# A versão 17 tem que acompanhar o servidor: o pg_dump recusa dumpar um Postgres
# MAIOR que ele ("aborting because of server version mismatch") -- foi o que
# derrubou a primeira execução do Cron-BACKUP com o cliente 16 contra o Supabase
# 17.6. Subir o Postgres do Supabase de major exige subir este pacote junto.
RUN apk add --no-cache ca-certificates postgresql17-client

COPY --from=builder /app/main .
COPY database/migrate ./database/migrate

CMD ["./main"]
